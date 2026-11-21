package kbreviewmail

import (
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// RequestText is the subject and body of the email to a reviewer: who asked, about which text, their
// note and the link to the review. A review opened again after an edit says the text changed.
func RequestText(mail store.KBReviewEmail, link string) (string, string) {
	review := mail.Review
	asker := person(review.RequestedBy, review.RequestedByToken)
	text := describe(review)
	note := ""
	if review.Note != "" {
		note = fmt.Sprintf("Their note: \"%s\"\n\n", review.Note)
	}
	footer := "You get this email because you were asked to review it. It is in your Reviews list under Knowledge base too.\n"
	if review.SupersedesReviewID != nil {
		return "Review again: " + review.ArticleTitle,
			fmt.Sprintf("%s in %s changed after %s asked you to review it, so it needs your review again before it is published.\n\n%sReview the new text: %s\n\n%s",
				text, mail.WorkspaceName, asker, note, link, footer)
	}
	return fmt.Sprintf("%s asked you to review: %s", asker, review.ArticleTitle),
		fmt.Sprintf("%s asked you to review %s in %s before it is published.\n\n%sOpen the review: %s\n\n%s",
			asker, text, mail.WorkspaceName, note, link, footer)
}

// DecisionText is the subject and body of the email that tells the person who sent a text for review
// how it was decided, by whom, with their note.
func DecisionText(mail store.KBReviewEmail, link string) (string, string) {
	review := mail.Review
	decider := person(review.DecidedBy, review.DecidedByToken)
	subject, verb, next := decider+" approved: ", "approved", "You can publish it now."
	if review.State == domain.KBReviewChangesRequested {
		subject, verb, next = decider+" asked for changes: ", "asked for changes to", "Edit it and send it for review again."
	}
	subject += review.ArticleTitle
	note := ""
	if review.DecisionNote != "" {
		note = fmt.Sprintf("Their note: \"%s\"\n\n", review.DecisionNote)
	}
	return subject, fmt.Sprintf("%s %s %s, which you sent for review in %s. %s\n\n%sOpen it: %s\n\n"+
		"You get this email because you sent it for review.\n", decider, verb, describe(review), mail.WorkspaceName, next, note, link)
}

// describe names the text: the article, or its translation in one language.
func describe(review domain.KBReview) string {
	if review.Locale != nil {
		return fmt.Sprintf("the %s translation \"%s\"", *review.Locale, review.ArticleTitle)
	}
	return fmt.Sprintf("the article \"%s\"", review.ArticleTitle)
}

// person names who did it, and the agent token they did it through.
func person(who *domain.SavedReplyPerson, token *domain.SavedReplyToken) string {
	name := "A teammate"
	if who != nil && who.Name != "" {
		name = who.Name
	}
	if token != nil {
		name += " (an AI agent through " + token.Label + ")"
	}
	return name
}
