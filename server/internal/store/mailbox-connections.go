package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// maxOAuthStates is how many consents one person may have in flight; an older one stops working.
const maxOAuthStates = 5

// ErrOAuthStateNotFound means the consent state is unknown in this workspace, used already or expired.
var ErrOAuthStateNotFound = errors.New("consent state not found")

// ErrEmailChannelMissing means Gmail cannot be connected because the workspace has no such email channel.
var ErrEmailChannelMissing = errors.New("email channel is not connected")

// SealedGrant is a provider grant as stored: sealed by the caller, with the id of the sealing key.
type SealedGrant struct {
	KeyID  string
	Sealed []byte
	// Address is the account the grant belongs to, set on grants read back from storage.
	Address string
}

// OAuthState is a consent in flight: the state the provider sends back, the PKCE verifier and the
// email channel it connects. Only the state's SHA-256 is stored.
type OAuthState struct {
	State     string
	AgentID   string
	Verifier  string
	ChannelID string
}

func oauthStateHash(state string) []byte {
	sum := sha256.Sum256([]byte(state))
	return sum[:]
}

// CreateOAuthState starts a consent for the agent to connect an email channel: a random state and PKCE
// verifier (RFC 7636: 43 to 128 characters; 64 random bytes give 86). Expired states of the workspace
// are removed on the way.
func (s *Store) CreateOAuthState(ctx context.Context, workspaceID, channelID, agentID string, now, expires time.Time) (OAuthState, error) {
	state, err := randomURLToken(32)
	if err != nil {
		return OAuthState{}, err
	}
	verifier, err := randomURLToken(64)
	if err != nil {
		return OAuthState{}, err
	}
	stateHash := oauthStateHash(state)
	_, err = scoped(ctx, s, workspaceID, func(tx pgx.Tx) (struct{}, error) {
		if _, err := tx.Exec(ctx, `DELETE FROM mailbox_oauth_states WHERE workspace_id=$1 AND expires_at<=$2`, workspaceID, now); err != nil {
			return struct{}{}, err
		}
		var channel bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM email_channels WHERE workspace_id=$1 AND channel_id::text=$2)`, workspaceID, channelID).Scan(&channel); err != nil {
			return struct{}{}, err
		}
		if !channel {
			return struct{}{}, ErrEmailChannelMissing
		}
		if _, err := tx.Exec(ctx, `INSERT INTO mailbox_oauth_states(state_hash,workspace_id,agent_id,provider,code_verifier,expires_at,created_at,channel_id)
			VALUES($1,$2,$3,'google',$4,$5,$6,$7::uuid)`, stateHash, workspaceID, agentID, verifier, expires, now, channelID); err != nil {
			return struct{}{}, err
		}
		// A person keeps at most maxOAuthStates consents in flight: the new one and the newest others.
		_, err := tx.Exec(ctx, `DELETE FROM mailbox_oauth_states WHERE workspace_id=$1 AND agent_id=$2 AND state_hash<>$3 AND state_hash NOT IN (
				SELECT state_hash FROM mailbox_oauth_states WHERE workspace_id=$1 AND agent_id=$2 AND state_hash<>$3
				ORDER BY created_at DESC LIMIT $4)`, workspaceID, agentID, stateHash, maxOAuthStates-1)
		return struct{}{}, err
	})
	if errors.Is(err, ErrEmailChannelMissing) {
		return OAuthState{}, err
	}
	if err != nil {
		return OAuthState{}, fmt.Errorf("record consent state: %w", err)
	}
	return OAuthState{State: state, AgentID: agentID, Verifier: verifier, ChannelID: channelID}, nil
}

// TakeOAuthState uses up a consent state of this workspace; an expired one is removed and refused. It is
// looked up inside the workspace, so a state started in another workspace is not found. A state of a
// workspace that had no email channel at the upgrade names no channel (ChannelID ""): it cannot finish.
func (s *Store) TakeOAuthState(ctx context.Context, workspaceID, state string, now time.Time) (OAuthState, error) {
	stateHash := oauthStateHash(state)
	type taken struct {
		state OAuthState
		live  bool
	}
	result, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (taken, error) {
		var result taken
		err := tx.QueryRow(ctx, `DELETE FROM mailbox_oauth_states WHERE workspace_id=$1 AND state_hash=$2
			RETURNING agent_id::text,code_verifier,expires_at>$3,COALESCE(channel_id::text,'')`,
			workspaceID, stateHash, now).Scan(&result.state.AgentID, &result.state.Verifier, &result.live, &result.state.ChannelID)
		return result, err
	})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !result.live) {
		return OAuthState{}, ErrOAuthStateNotFound
	}
	if err != nil {
		return OAuthState{}, fmt.Errorf("take consent state: %w", err)
	}
	return result.state, nil
}

const mailboxColumns = `provider,address,status,error_code,failed_at,connected_at`

func scanMailbox(row pgx.Row) (domain.MailboxConnection, error) {
	var connection domain.MailboxConnection
	if err := row.Scan(&connection.Provider, &connection.Address, &connection.Status, &connection.ErrorCode, &connection.FailedAt, &connection.ConnectedAt); err != nil {
		return domain.MailboxConnection{}, err
	}
	if connection.ErrorCode != nil {
		text := domain.MailboxFailureText(*connection.ErrorCode)
		connection.Error = &text
	}
	connection.FailedAt = domain.UTCOptional(connection.FailedAt)
	connection.ConnectedAt = domain.UTC(connection.ConnectedAt)
	return connection, nil
}

// GetMailboxConnection returns the Gmail account connected to an email channel, or nil.
func (s *Store) GetMailboxConnection(ctx context.Context, workspaceID, channelID string) (*domain.MailboxConnection, error) {
	connection, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.MailboxConnection, error) {
		return scanMailbox(tx.QueryRow(ctx, `SELECT `+mailboxColumns+` FROM mailbox_connections WHERE workspace_id=$1 AND channel_id::text=$2`, workspaceID, channelID))
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get mailbox connection: %w", err)
	}
	return &connection, nil
}

// SaveMailboxConnection connects the account to an email channel, replacing any earlier grant of it. It
// returns the grant it replaced (nil for none) so the caller can revoke it at the provider once this is
// committed.
func (s *Store) SaveMailboxConnection(ctx context.Context, workspaceID, channelID, agentID, address, scopes string, grant SealedGrant, now time.Time) (domain.MailboxConnection, *SealedGrant, error) {
	type saved struct {
		connection domain.MailboxConnection
		replaced   *SealedGrant
	}
	result, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (saved, error) {
		var result saved
		var old SealedGrant
		err := tx.QueryRow(ctx, `SELECT token_key_id,refresh_token_sealed,address FROM mailbox_connections WHERE workspace_id=$1 AND channel_id::text=$2 FOR UPDATE`,
			workspaceID, channelID).Scan(&old.KeyID, &old.Sealed, &old.Address)
		switch {
		case err == nil:
			result.replaced = &old
		case !errors.Is(err, pgx.ErrNoRows):
			return result, err
		}
		result.connection, err = scanMailbox(tx.QueryRow(ctx, `INSERT INTO mailbox_connections(workspace_id,provider,address,scopes,token_key_id,refresh_token_sealed,connected_by_agent_id,connected_at,updated_at,channel_id)
			VALUES($1,'google',lower($2),$3,$4,$5,NULLIF($6,'')::uuid,$7,$7,$8::uuid)
			ON CONFLICT(channel_id) DO UPDATE SET provider=EXCLUDED.provider,address=EXCLUDED.address,scopes=EXCLUDED.scopes,token_key_id=EXCLUDED.token_key_id,
				refresh_token_sealed=EXCLUDED.refresh_token_sealed,status='connected',error_code=NULL,failed_at=NULL,
				connected_by_agent_id=EXCLUDED.connected_by_agent_id,connected_at=EXCLUDED.connected_at,updated_at=EXCLUDED.updated_at
			RETURNING `+mailboxColumns, workspaceID, address, scopes, grant.KeyID, grant.Sealed, agentID, now, channelID))
		return result, err
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.Code == "23503" && pgErr.ConstraintName == "mailbox_connections_channel_fkey" || pgErr.Code == "22P02") {
		return domain.MailboxConnection{}, nil, ErrEmailChannelMissing
	}
	if err != nil {
		return domain.MailboxConnection{}, nil, fmt.Errorf("save mailbox connection: %w", err)
	}
	return result.connection, result.replaced, nil
}

// MailboxGrant returns the grant stored for an email channel, for revoking it before the connection or
// channel is removed.
func (s *Store) MailboxGrant(ctx context.Context, workspaceID, channelID string) (*SealedGrant, error) {
	grant, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (SealedGrant, error) {
		var grant SealedGrant
		err := tx.QueryRow(ctx, `SELECT token_key_id,refresh_token_sealed,address FROM mailbox_connections WHERE workspace_id=$1 AND channel_id::text=$2`,
			workspaceID, channelID).Scan(&grant.KeyID, &grant.Sealed, &grant.Address)
		return grant, err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read mailbox grant: %w", err)
	}
	return &grant, nil
}

// DeleteMailboxConnection disconnects Gmail from an email channel and returns the grant it removed (nil
// when none was connected).
func (s *Store) DeleteMailboxConnection(ctx context.Context, workspaceID, channelID string) (*SealedGrant, error) {
	grant, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (SealedGrant, error) {
		var grant SealedGrant
		err := tx.QueryRow(ctx, `DELETE FROM mailbox_connections WHERE workspace_id=$1 AND channel_id::text=$2 RETURNING token_key_id,refresh_token_sealed,address`,
			workspaceID, channelID).Scan(&grant.KeyID, &grant.Sealed, &grant.Address)
		return grant, err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("delete mailbox connection: %w", err)
	}
	return &grant, nil
}

// MailboxChannelFor returns the email channel of the workspace whose Gmail connection is the account,
// connected or failed; "" when none.
func (s *Store) MailboxChannelFor(ctx context.Context, workspaceID, address string) (string, error) {
	id, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (string, error) {
		var id string
		err := tx.QueryRow(ctx, `SELECT channel_id::text FROM mailbox_connections WHERE workspace_id=$1 AND address=lower($2)`, workspaceID, address).Scan(&id)
		return id, err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("find gmail account: %w", err)
	}
	return id, nil
}

// MailboxAddressHeldElsewhere reports whether another email channel, of this workspace or another, holds a
// Gmail connection for the address, connected or failed: Google revokes the whole grant, so revoking would
// cut that channel off too. Call it only after this channel's own row is gone and committed: two channels
// dropping the address together then cannot both see the other and both skip the revoke.
func (s *Store) MailboxAddressHeldElsewhere(ctx context.Context, channelID, address string) (bool, error) {
	var held bool
	err := s.db.Privileged(ctx, database.PrivilegeMailboxGrantShared, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM mailbox_connections WHERE address=lower($1) AND channel_id::text<>$2)`, address, channelID).Scan(&held)
	})
	if err != nil {
		return false, fmt.Errorf("check shared mailbox grant: %w", err)
	}
	return held, nil
}

// MarkMailboxFailed records why the connection stopped working, only while it still holds the grant the
// caller used: a reconnect that raced the failure is not marked.
func (s *Store) MarkMailboxFailed(ctx context.Context, workspaceID string, used SealedGrant, code string, now time.Time) error {
	_, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (struct{}, error) {
		_, err := tx.Exec(ctx, `UPDATE mailbox_connections SET status='failed',error_code=$4,failed_at=$5,updated_at=$5
			WHERE workspace_id=$1 AND token_key_id=$2 AND refresh_token_sealed=$3 AND status='connected'`, workspaceID, used.KeyID, used.Sealed, code, now)
		return struct{}{}, err
	})
	if err != nil {
		return fmt.Errorf("mark mailbox connection failed: %w", err)
	}
	return nil
}

// ReplaceMailboxGrant stores a grant sealed again (a refresh token the provider rotated, or the same token
// under the current key), only while the old one is still stored. It reports whether it replaced it.
func (s *Store) ReplaceMailboxGrant(ctx context.Context, workspaceID string, old, replacement SealedGrant, now time.Time) (bool, error) {
	replaced, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (bool, error) {
		result, err := tx.Exec(ctx, `UPDATE mailbox_connections SET token_key_id=$4,refresh_token_sealed=$5,updated_at=$6
			WHERE workspace_id=$1 AND token_key_id=$2 AND refresh_token_sealed=$3`, workspaceID, old.KeyID, old.Sealed, replacement.KeyID, replacement.Sealed, now)
		return result.RowsAffected() == 1, err
	})
	if err != nil {
		return false, fmt.Errorf("replace mailbox grant: %w", err)
	}
	return replaced, nil
}
