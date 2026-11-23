package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// helpCenterLocale reads the primary language of help center $2 of workspace $1 ("" for none, 'en').
const helpCenterLocale = `SELECT COALESCE((SELECT locale FROM help_centers WHERE workspace_id=$1 AND id=NULLIF($2,'')::uuid), 'en')`

// readHelpCenterLanguages reads the primary language of help center helpCenterID, the languages it
// lists with their counts, and the locales translations exist in that are neither.
func readHelpCenterLanguages(ctx context.Context, tx pgx.Tx, workspaceID, helpCenterID string) (domain.HelpCenterLanguages, error) {
	result := domain.HelpCenterLanguages{Languages: []domain.HelpCenterLanguage{}, Unlisted: []string{}}
	if err := tx.QueryRow(ctx, helpCenterLocale, workspaceID, helpCenterID).Scan(&result.Primary); err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, `WITH live AS (SELECT count(*) AS n FROM kb_articles WHERE workspace_id=$1 AND deleted_at IS NULL),
			counts AS (SELECT t.locale, count(*) FILTER (WHERE t.status='published') AS published, count(*) FILTER (WHERE t.status='draft') AS drafts,
				count(*) FILTER (WHERE t.outdated) AS outdated, count(*) AS translated
				FROM kb_article_translations t JOIN kb_articles a ON a.workspace_id=t.workspace_id AND a.id=t.article_id AND a.deleted_at IS NULL
				WHERE t.workspace_id=$1 AND t.deleted_at IS NULL GROUP BY t.locale)
		SELECT l.locale, l.visible, l.position, COALESCE(c.published,0), COALESCE(c.drafts,0), COALESCE(c.outdated,0), (SELECT n FROM live) - COALESCE(c.translated,0)
		FROM help_center_languages l LEFT JOIN counts c ON c.locale=l.locale
		WHERE l.workspace_id=$1 AND l.help_center_id=NULLIF($3,'')::uuid AND l.locale <> $2 ORDER BY l.position, l.locale`, workspaceID, result.Primary, helpCenterID)
	if err != nil {
		return result, err
	}
	if result.Languages, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.HelpCenterLanguage, error) {
		var item domain.HelpCenterLanguage
		err := row.Scan(&item.Locale, &item.Visible, &item.Position, &item.Published, &item.Drafts, &item.Outdated, &item.Missing)
		return item, err
	}); err != nil {
		return result, err
	}
	if result.Languages == nil {
		result.Languages = []domain.HelpCenterLanguage{}
	}
	rows, err = tx.Query(ctx, `SELECT locale FROM kb_article_translations WHERE workspace_id=$1
			UNION SELECT locale FROM kb_collection_translations WHERE workspace_id=$1
		EXCEPT SELECT locale FROM help_center_languages WHERE workspace_id=$1 AND help_center_id=NULLIF($3,'')::uuid AND locale <> $2 ORDER BY 1`,
		workspaceID, result.Primary, helpCenterID)
	if err != nil {
		return result, err
	}
	unlisted, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return result, err
	}
	if unlisted != nil {
		result.Unlisted = unlisted
	}
	return result, nil
}

// GetHelpCenterLanguages returns the primary help center's languages (GetHelpCenterLanguagesIn).
func (s *Store) GetHelpCenterLanguages(ctx context.Context, workspaceID string) (domain.HelpCenterLanguages, error) {
	return s.GetHelpCenterLanguagesIn(ctx, workspaceID, "")
}

// GetHelpCenterLanguagesIn returns the languages of help center helpCenterID ("" the primary one).
func (s *Store) GetHelpCenterLanguagesIn(ctx context.Context, workspaceID, helpCenterID string) (domain.HelpCenterLanguages, error) {
	result, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.HelpCenterLanguages, error) {
		target, err := resolveHelpCenter(ctx, tx, workspaceID, helpCenterID, false)
		if err != nil {
			return domain.HelpCenterLanguages{}, err
		}
		return readHelpCenterLanguages(ctx, tx, workspaceID, target)
	})
	return result, helpCenterStoreError("read help center languages", err)
}

// HelpCenterLanguagesInUseError names the languages a write would drop that still hold translations.
type HelpCenterLanguagesInUseError struct{ Locales []string }

func (e *HelpCenterLanguagesInUseError) Error() string {
	return "languages still hold translations: " + strings.Join(e.Locales, ", ")
}

// SetHelpCenterLanguages sets the primary help center's languages (SetHelpCenterLanguagesIn).
func (s *Store) SetHelpCenterLanguages(ctx context.Context, workspaceID string, languages []domain.HelpCenterLanguageInput) (domain.HelpCenterLanguages, error) {
	return s.SetHelpCenterLanguagesIn(ctx, workspaceID, "", languages)
}

// SetHelpCenterLanguagesIn makes languages, in their order, the languages of help center helpCenterID
// ("" the primary one) besides its primary one. A language left out that still holds a translation or
// a collection name (in the trash too) is refused unless another live help center of the workspace
// still lists it, so no translation is ever left without its language by this call.
func (s *Store) SetHelpCenterLanguagesIn(ctx context.Context, workspaceID, helpCenterID string, languages []domain.HelpCenterLanguageInput) (domain.HelpCenterLanguages, error) {
	locales, visible, err := checkHelpCenterLanguages(languages)
	if err != nil {
		return domain.HelpCenterLanguages{}, err
	}
	result, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.HelpCenterLanguages, error) {
		if err := lockKB(ctx, tx, workspaceID); err != nil {
			return domain.HelpCenterLanguages{}, err
		}
		target, err := resolveHelpCenter(ctx, tx, workspaceID, helpCenterID, true)
		if err != nil {
			return domain.HelpCenterLanguages{}, err
		}
		var primary string
		if err := tx.QueryRow(ctx, helpCenterLocale, workspaceID, target).Scan(&primary); err != nil {
			return domain.HelpCenterLanguages{}, err
		}
		if slices.ContainsFunc(locales, func(locale string) bool { return strings.EqualFold(locale, primary) }) {
			return domain.HelpCenterLanguages{}, &domain.InvalidKBError{Message: primary + " is the primary language of the help center (the articles' own language); list only the other languages."}
		}
		rows, err := tx.Query(ctx, `SELECT DISTINCT l.locale FROM help_center_languages l WHERE l.workspace_id=$1 AND l.help_center_id=$4 AND l.locale <> ALL($2::text[]) AND l.locale <> $3
			AND (EXISTS (SELECT 1 FROM kb_article_translations t WHERE t.workspace_id=l.workspace_id AND t.locale=l.locale)
				OR EXISTS (SELECT 1 FROM kb_collection_translations t WHERE t.workspace_id=l.workspace_id AND t.locale=l.locale))
			AND NOT EXISTS (SELECT 1 FROM help_center_languages o JOIN help_centers h ON h.workspace_id=o.workspace_id AND h.id=o.help_center_id AND h.deleted_at IS NULL
				WHERE o.workspace_id=l.workspace_id AND o.help_center_id <> $4 AND o.locale=l.locale) ORDER BY 1`, workspaceID, locales, primary, target)
		if err != nil {
			return domain.HelpCenterLanguages{}, err
		}
		inUse, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return domain.HelpCenterLanguages{}, err
		}
		if len(inUse) > 0 {
			return domain.HelpCenterLanguages{}, &HelpCenterLanguagesInUseError{Locales: inUse}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM help_center_languages WHERE workspace_id=$1 AND help_center_id=$3 AND locale <> ALL($2::text[])`, workspaceID, locales, target); err != nil {
			return domain.HelpCenterLanguages{}, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO help_center_languages(workspace_id,help_center_id,locale,visible,position)
			SELECT $1, $4, x.locale, x.visible, x.position FROM unnest($2::text[], $3::boolean[]) WITH ORDINALITY AS x(locale, visible, position)
			ON CONFLICT (workspace_id, help_center_id, locale) DO UPDATE SET visible=EXCLUDED.visible, position=EXCLUDED.position,
				updated_at=CASE WHEN (help_center_languages.visible, help_center_languages.position) IS DISTINCT FROM (EXCLUDED.visible, EXCLUDED.position)
					THEN clock_timestamp() ELSE help_center_languages.updated_at END`, workspaceID, locales, visible, target); err != nil {
			return domain.HelpCenterLanguages{}, err
		}
		if err := bumpKB(ctx, tx, workspaceID); err != nil {
			return domain.HelpCenterLanguages{}, err
		}
		return readHelpCenterLanguages(ctx, tx, workspaceID, target)
	})
	if err != nil {
		var inUse *HelpCenterLanguagesInUseError
		if errors.As(err, &inUse) || errors.Is(err, domain.ErrHelpCenterNotFound) {
			return result, err
		}
		return result, kbError("set help center languages", err)
	}
	return result, nil
}

// checkHelpCenterLanguages checks the bound, each locale and duplicates, and splits languages into
// their locales and visibility.
func checkHelpCenterLanguages(languages []domain.HelpCenterLanguageInput) (locales []string, visible []bool, err error) {
	if len(languages) > domain.MaxHelpCenterLanguages {
		return nil, nil, &domain.InvalidKBError{Message: fmt.Sprintf("A help center has at most %d languages besides its primary one.", domain.MaxHelpCenterLanguages)}
	}
	locales, visible = make([]string, 0, len(languages)), make([]bool, 0, len(languages))
	for _, language := range languages {
		if err := domain.CheckLocale(language.Locale); err != nil {
			return nil, nil, err
		}
		for _, seen := range locales {
			if strings.EqualFold(seen, language.Locale) {
				return nil, nil, &domain.InvalidKBError{Message: "Each language is listed once; " + language.Locale + " is listed twice."}
			}
		}
		locales, visible = append(locales, language.Locale), append(visible, language.Visible)
	}
	return locales, visible, nil
}

// HelpCenterLanguagesShownChange tells whether setting languages on help center helpCenterID ("" the
// primary one) would show a language customers do not see now, or hide one they do (a change to the
// public help center).
func (s *Store) HelpCenterLanguagesShownChange(ctx context.Context, workspaceID, helpCenterID string, languages []domain.HelpCenterLanguageInput) (bool, error) {
	shown := []string{}
	for _, language := range languages {
		if language.Visible {
			shown = append(shown, language.Locale)
		}
	}
	changed, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (bool, error) {
		target, err := resolveHelpCenter(ctx, tx, workspaceID, helpCenterID, false)
		if err != nil {
			return false, err
		}
		rows, err := tx.Query(ctx, `SELECT locale FROM help_center_languages WHERE workspace_id=$1 AND help_center_id=NULLIF($2,'')::uuid AND visible ORDER BY 1`, workspaceID, target)
		if err != nil {
			return false, err
		}
		now, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return false, err
		}
		slices.Sort(shown)
		return !slices.Equal(now, shown), nil
	})
	return changed, helpCenterStoreError("compare help center languages", err)
}
