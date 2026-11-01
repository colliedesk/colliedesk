package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5"
)

// InboundEvent is one row of a channel's journal.
type InboundEvent struct {
	ID, WorkspaceID, ChannelID string
	ExternalEventID, State     string
	Raw                        json.RawMessage
	ObjectKey                  string
	HoldReason                 string
	ReceivedAt                 time.Time
}

// RecordInboundEvent commits a raw event of an Async channel before it is processed, so nothing is
// lost when processing fails. An event the channel already sent (same external id) is a duplicate.
func (s *Store) RecordInboundEvent(ctx context.Context, workspaceID, channelID string, raw channels.Raw, now time.Time) (id string, duplicate bool, err error) {
	if raw.JSON == nil {
		return "", false, errors.New("an Async channel's event must be JSON")
	}
	err = s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `INSERT INTO channel_inbound_events(workspace_id,channel_id,external_event_id,state,raw,received_at)
			VALUES($1,$2,NULLIF($3,''),'received',$4,$5) ON CONFLICT (workspace_id,channel_id,external_event_id) WHERE external_event_id IS NOT NULL DO NOTHING
			RETURNING id::text`, workspaceID, channelID, raw.ExternalEventID, raw.JSON, now).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			duplicate = true
			return tx.QueryRow(ctx, `SELECT id::text FROM channel_inbound_events WHERE workspace_id=$1 AND channel_id=$2 AND external_event_id=$3`,
				workspaceID, channelID, raw.ExternalEventID).Scan(&id)
		}
		return err
	})
	if err != nil {
		return "", false, fmt.Errorf("record channel event: %w", err)
	}
	return id, duplicate, nil
}

// InboundWork is a set of journal events of one channel to process together: one received or
// failed event, or the collected events of one key whose time came.
type InboundWork struct {
	WorkspaceID, ChannelID string
	EventIDs               []string
	Flush                  bool
}

// DueInbound lists journal events due for processing in every workspace: received or failed ones
// under maxAttempts, and collected groups whose flush time passed. Processing locks each event, so
// two workers never process one twice.
func (s *Store) DueInbound(ctx context.Context, now time.Time, limit, maxAttempts int) ([]InboundWork, error) {
	var work []InboundWork
	err := s.db.Privileged(ctx, database.PrivilegeChannelInboundClaim, func(tx pgx.Tx) error {
		// A paused channel's events wait where they are.
		rows, err := tx.Query(ctx, `(SELECT e.workspace_id::text,e.channel_id::text,ARRAY[e.id::text],false FROM channel_inbound_events e
				WHERE e.state IN ('received','failed') AND e.attempts<$2
					AND NOT EXISTS (SELECT 1 FROM channels c WHERE c.workspace_id=e.workspace_id AND c.id=e.channel_id AND c.processing_paused_at IS NOT NULL)
				ORDER BY e.received_at LIMIT $3)
			UNION ALL
			(SELECT e.workspace_id::text,e.channel_id::text,array_agg(e.id::text ORDER BY e.received_at,e.id),true FROM channel_inbound_events e
				WHERE e.state='pending'
					AND NOT EXISTS (SELECT 1 FROM channels c WHERE c.workspace_id=e.workspace_id AND c.id=e.channel_id AND c.processing_paused_at IS NOT NULL)
				GROUP BY e.workspace_id,e.channel_id,e.aggregate_key HAVING max(e.flush_at)<=$1 LIMIT $3)`, now, maxAttempts, limit)
		if err != nil {
			return err
		}
		work, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (InboundWork, error) {
			var item InboundWork
			err := row.Scan(&item.WorkspaceID, &item.ChannelID, &item.EventIDs, &item.Flush)
			return item, err
		})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list due channel events: %w", err)
	}
	return work, nil
}

// PausedInbound lists events of paused channels whose files were not fetched yet, oldest first: their
// links expire, so their files are fetched while the rest waits.
func (s *Store) PausedInbound(ctx context.Context, limit int) ([]InboundWork, error) {
	var work []InboundWork
	err := s.db.Privileged(ctx, database.PrivilegeChannelInboundClaim, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT e.workspace_id::text,e.channel_id::text,ARRAY[e.id::text],false FROM channel_inbound_events e
			JOIN channels c ON c.workspace_id=e.workspace_id AND c.id=e.channel_id AND c.processing_paused_at IS NOT NULL
			WHERE e.state IN ('received','failed') AND e.files_fetched_at IS NULL ORDER BY e.received_at LIMIT $1`, limit)
		if err != nil {
			return err
		}
		work, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (InboundWork, error) {
			var item InboundWork
			err := row.Scan(&item.WorkspaceID, &item.ChannelID, &item.EventIDs, &item.Flush)
			return item, err
		})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list paused channel events: %w", err)
	}
	return work, nil
}

// MarkFilesFetched records that an event's files were fetched while its channel was paused.
func (s *Store) MarkFilesFetched(ctx context.Context, workspaceID string, ids []string) error {
	_, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (struct{}, error) {
		_, err := tx.Exec(ctx, `UPDATE channel_inbound_events SET files_fetched_at=now() WHERE workspace_id=$1 AND id::text=ANY($2)`, workspaceID, ids)
		return struct{}{}, err
	})
	return err
}

// InboundEvents reads journal events of a channel of the workspace, in the order they arrived.
func (s *Store) InboundEvents(ctx context.Context, workspaceID, channelID string, ids []string) ([]InboundEvent, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]InboundEvent, error) {
		rows, err := tx.Query(ctx, `SELECT id::text,workspace_id::text,channel_id::text,COALESCE(external_event_id,''),state,raw,COALESCE(raw_object_key,''),
			COALESCE(hold_reason,''),received_at FROM channel_inbound_events WHERE workspace_id=$1 AND channel_id=$2 AND id::text=ANY($3) ORDER BY received_at,id`,
			workspaceID, channelID, ids)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, scanInboundEvent)
	})
}

// GatheringEvents reads the held events of a gathering still to be decided, oldest first, with its
// channel; ErrGatheringNotFound when there is none, ErrGatheringDecided when it was decided.
func (s *Store) GatheringEvents(ctx context.Context, workspaceID, id string) (string, []InboundEvent, error) {
	type found struct {
		channelID string
		events    []InboundEvent
	}
	if !isID(id) {
		return "", nil, ErrGatheringNotFound
	}
	got, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (found, error) {
		var f found
		var state string
		err := tx.QueryRow(ctx, `SELECT channel_id::text,state FROM intake_gatherings WHERE workspace_id=$1 AND id=$2::uuid`, workspaceID, id).Scan(&f.channelID, &state)
		if errors.Is(err, pgx.ErrNoRows) {
			return f, ErrGatheringNotFound
		}
		if err != nil {
			return f, err
		}
		if state != GatheringDeciding && state != GatheringReview {
			return f, ErrGatheringDecided
		}
		rows, err := tx.Query(ctx, `SELECT id::text,workspace_id::text,channel_id::text,COALESCE(external_event_id,''),state,raw,COALESCE(raw_object_key,''),
			COALESCE(hold_reason,''),received_at FROM channel_inbound_events WHERE workspace_id=$1 AND gathering_id=$2::uuid ORDER BY received_at,id`, workspaceID, id)
		if err != nil {
			return f, err
		}
		f.events, err = pgx.CollectRows(rows, scanInboundEvent)
		return f, err
	})
	return got.channelID, got.events, err
}

func scanInboundEvent(row pgx.CollectableRow) (InboundEvent, error) {
	var event InboundEvent
	err := row.Scan(&event.ID, &event.WorkspaceID, &event.ChannelID, &event.ExternalEventID, &event.State, &event.Raw, &event.ObjectKey,
		&event.HoldReason, &event.ReceivedAt)
	return event, err
}

// inIntakeQueue keeps out of the held messages a person opens or discards one by one those of a chat's
// intake that wait to be decided, or were set aside: they are decided as a gathering, in the queue.
const inIntakeQueue = `NOT EXISTS(SELECT 1 FROM intake_gatherings g WHERE g.workspace_id=e.workspace_id AND g.id=e.gathering_id AND g.state IN `

// HeldEvents lists the events a channel holds (all, or those of ids), oldest first, but those a
// gathering of intake decides.
func (s *Store) HeldEvents(ctx context.Context, workspaceID, channelID string, ids []string) ([]InboundEvent, error) {
	if ids == nil {
		ids = []string{}
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]InboundEvent, error) {
		rows, err := tx.Query(ctx, `SELECT id::text,workspace_id::text,channel_id::text,COALESCE(external_event_id,''),state,raw,COALESCE(raw_object_key,''),
			COALESCE(hold_reason,''),received_at FROM channel_inbound_events e
			WHERE workspace_id=$1 AND channel_id=$2 AND state='held' AND (cardinality($3::text[])=0 OR id::text=ANY($3))
			AND `+inIntakeQueue+`('deciding','review','ignored')) ORDER BY received_at,id`,
			workspaceID, channelID, ids)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, scanInboundEvent)
	})
}

// MarkInboundFailed records why processing events failed; the worker tries again.
func (s *Store) MarkInboundFailed(ctx context.Context, workspaceID string, ids []string, reason string) error {
	if len(reason) > 500 {
		reason = reason[:500]
	}
	return s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE channel_inbound_events SET state='failed',error=$3,attempts=attempts+1,flush_at=NULL,aggregate_key=aggregate_key
			WHERE workspace_id=$1 AND id::text=ANY($2) AND state IN ('received','failed','pending')`, workspaceID, ids, reason)
		return err
	})
}

// RecordHeldReplay adds one history entry for held events an admin opened.
func (s *Store) RecordHeldReplay(ctx context.Context, workspaceID, channelID string, count int, conversationID string, actor ChannelActor) error {
	_, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (struct{}, error) {
		channel, err := s.channelByID(ctx, tx, workspaceID, channelID)
		if err != nil {
			return struct{}{}, err
		}
		detail := map[string]any{"count": count}
		if conversationID != "" {
			detail["conversation_id"] = conversationID
		}
		return struct{}{}, insertChannelEvent(ctx, tx, channel, "held_replayed", actor, detail)
	})
	return err
}

// DiscardHeld throws away events a channel holds (all, or those of ids); their raw objects are
// deleted when nothing else uses them. It returns how many were discarded.
func (s *Store) DiscardHeld(ctx context.Context, workspaceID, channelID string, ids []string, actor ChannelActor) (int, error) {
	if ids == nil {
		ids = []string{}
	}
	type discarded struct {
		count int
		keys  []string
	}
	got, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (discarded, error) {
		channel, err := s.channelByID(ctx, tx, workspaceID, channelID)
		if err != nil {
			return discarded{}, err
		}
		// What intake set aside may be thrown away; what waits to be decided is decided in the queue.
		rows, err := tx.Query(ctx, `UPDATE channel_inbound_events e SET state='discarded',hold_reason=NULL,decided_by_agent_id=NULLIF($4,'')::uuid,processed_at=now()
			WHERE workspace_id=$1 AND channel_id=$2 AND state='held' AND (cardinality($3::text[])=0 OR id::text=ANY($3))
			AND `+inIntakeQueue+`('deciding','review'))
			RETURNING COALESCE(raw_object_key,'')`, workspaceID, channelID, ids, actor.AgentID)
		if err != nil {
			return discarded{}, fmt.Errorf("discard held messages: %w", err)
		}
		keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return discarded{}, err
		}
		count := len(keys)
		// A gathering set aside whose held messages are all thrown away is over. A card of messages taken
		// back to the queue holds no held message: it is never ended here.
		if _, err := tx.Exec(ctx, `UPDATE intake_gatherings g SET state='cancelled',updated_at=now() WHERE g.workspace_id=$1 AND g.channel_id=$2 AND g.state='ignored'
			AND EXISTS(SELECT 1 FROM channel_inbound_events e WHERE e.workspace_id=g.workspace_id AND e.gathering_id=g.id)
			AND NOT EXISTS(SELECT 1 FROM channel_inbound_events e WHERE e.workspace_id=g.workspace_id AND e.gathering_id=g.id AND e.state<>'discarded')`,
			workspaceID, channelID); err != nil {
			return discarded{}, fmt.Errorf("end discarded gatherings: %w", err)
		}
		// What a discarded message said goes from its chat place too (and with it any link).
		if _, err := tx.Exec(ctx, `DELETE FROM channel_thread_items i USING channel_inbound_events e
			WHERE i.workspace_id=$1 AND e.workspace_id=i.workspace_id AND e.id=i.inbound_event_id AND e.channel_id=$2 AND e.state='discarded'`,
			workspaceID, channelID); err != nil {
			return discarded{}, fmt.Errorf("discard held chat items: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM channel_senders s WHERE s.workspace_id=$1 AND s.channel_id=$2 AND NOT EXISTS(SELECT 1 FROM channel_thread_items i
			WHERE i.workspace_id=s.workspace_id AND i.channel_id=s.channel_id AND i.sender_kind=s.sender_kind AND i.sender_value=s.sender_value)`,
			workspaceID, channelID); err != nil {
			return discarded{}, fmt.Errorf("discard held chat senders: %w", err)
		}
		if count > 0 {
			if err := insertChannelEvent(ctx, tx, channel, "held_discarded", actor, map[string]any{"count": count}); err != nil {
				return discarded{}, err
			}
		}
		// Their fetched files go too, and stop counting toward storage.
		fileRows, err := tx.Query(ctx, `DELETE FROM channel_inbound_files f USING channel_inbound_events e
			WHERE f.workspace_id=$1 AND e.workspace_id=f.workspace_id AND e.id=f.event_id AND e.channel_id=$2 AND e.state='discarded'
			AND f.status='stored' AND NOT f.attached RETURNING f.storage_key`, workspaceID, channelID)
		if err != nil {
			return discarded{}, fmt.Errorf("discard held files: %w", err)
		}
		fileKeys, err := pgx.CollectRows(fileRows, pgx.RowTo[string])
		if err != nil {
			return discarded{}, err
		}
		return discarded{count: count, keys: append(keys, fileKeys...)}, nil
	})
	if err != nil {
		return 0, err
	}
	for _, key := range got.keys {
		if key != "" && s.objects != nil {
			if err := s.DiscardObject(ctx, workspaceID, key); err != nil {
				return got.count, err
			}
		}
	}
	return got.count, nil
}

// InboundFileState is one file of a journal event as the fetch step sees it.
type InboundFileState struct {
	Index    int
	Status   string
	Attempts int
}

// RecordInboundFiles lists the files of an event the first time it is processed; later runs keep
// what an earlier one fetched.
func (s *Store) RecordInboundFiles(ctx context.Context, workspaceID, eventID string, refs []channels.FileRef) ([]InboundFileState, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]InboundFileState, error) {
		for index, ref := range refs {
			name, contentType := ref.Name, ref.ContentType
			if name == "" {
				name = fmt.Sprintf("file-%d", index+1)
			}
			if contentType == "" {
				contentType = "application/octet-stream"
			}
			if _, err := tx.Exec(ctx, `INSERT INTO channel_inbound_files(workspace_id,event_id,idx,name,content_type,status)
				VALUES($1,$2,$3,left($4,255),left($5,255),'pending') ON CONFLICT DO NOTHING`, workspaceID, eventID, index, name, contentType); err != nil {
				return nil, fmt.Errorf("list event files: %w", err)
			}
		}
		rows, err := tx.Query(ctx, `SELECT idx,status,attempts FROM channel_inbound_files WHERE workspace_id=$1 AND event_id=$2 ORDER BY idx`, workspaceID, eventID)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(row pgx.CollectableRow) (InboundFileState, error) {
			var state InboundFileState
			err := row.Scan(&state.Index, &state.Status, &state.Attempts)
			return state, err
		})
	})
}

// FinishInboundFile records a fetched file, or why it could not be fetched.
func (s *Store) FinishInboundFile(ctx context.Context, workspaceID, eventID string, index int, object *attachments.Object, attempts int, failure string) error {
	return s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		if object != nil {
			if err := VerifyObjects(ctx, tx, s.objects, object.Key); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `UPDATE channel_inbound_files SET status='stored',storage_key=$4,sha256=$5,size=$6,attempts=$7,error=NULL,updated_at=now()
				WHERE workspace_id=$1 AND event_id=$2 AND idx=$3 AND status='pending'`, workspaceID, eventID, index, object.Key, object.SHA256, object.Size, attempts)
			return err
		}
		if len(failure) > 500 {
			failure = failure[:500]
		}
		_, err := tx.Exec(ctx, `UPDATE channel_inbound_files SET status='failed',error=$4,attempts=$5,updated_at=now()
			WHERE workspace_id=$1 AND event_id=$2 AND idx=$3 AND status='pending'`, workspaceID, eventID, index, failure, attempts)
		return err
	})
}

// InboundFiles reads the fetched files of events, by event id.
func (s *Store) InboundFiles(ctx context.Context, workspaceID string, eventIDs []string) (map[string][]channels.FetchedFile, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (map[string][]channels.FetchedFile, error) {
		rows, err := tx.Query(ctx, `SELECT event_id::text,idx,name,content_type,status,COALESCE(storage_key,''),COALESCE(sha256,''),COALESCE(size,0),COALESCE(error,'')
			FROM channel_inbound_files WHERE workspace_id=$1 AND event_id::text=ANY($2) ORDER BY event_id,idx`, workspaceID, eventIDs)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		files := map[string][]channels.FetchedFile{}
		for rows.Next() {
			var eventID, status, key, sum, failure string
			var size int64
			var file channels.FetchedFile
			if err := rows.Scan(&eventID, &file.Index, &file.Name, &file.ContentType, &status, &key, &sum, &size, &failure); err != nil {
				return nil, err
			}
			switch status {
			case "stored":
				file.Object = &attachments.Object{Key: key, SHA256: sum, Size: size}
			case "failed":
				file.Error = failure
			default:
				file.Error = "the file was not fetched"
			}
			files[eventID] = append(files[eventID], file)
		}
		return files, rows.Err()
	})
}

// CheckChannelFileRoom refuses a fetched file the workspace's storage has no room for.
func (s *Store) CheckChannelFileRoom(ctx context.Context, workspaceID string, size int64) error {
	_, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (struct{}, error) {
		return struct{}{}, s.CheckInboundStorage(ctx, tx, workspaceID, size, s.now())
	})
	return err
}
