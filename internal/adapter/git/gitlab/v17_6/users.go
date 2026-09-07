package gitlabv176

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"

	"github.com/dhkimxx/crowsnest/internal/domain"
	"github.com/dhkimxx/crowsnest/internal/ports"
)

type gitLabUser struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	State    string `json:"state"`
	Locked   bool   `json:"locked"`
	External bool   `json:"external"`
	Bot      bool   `json:"bot"`
}

type gitLabUserEmail struct {
	Email       string `json:"email"`
	ConfirmedAt string `json:"confirmed_at"`
}

func (c *HookController) ListUsers(ctx context.Context) ([]ports.UserRecord, error) {
	var users []ports.UserRecord
	for page := 1; ; page++ {
		var pageUsers []gitLabUser
		nextPage, err := c.requestJSON(ctx, http.MethodGet, "/users?per_page=100&page="+strconv.Itoa(page)+"&order_by=id&sort=asc", nil, &pageUsers)
		if err != nil {
			return nil, err
		}
		for _, user := range pageUsers {
			if usableUserEmail(user.Email) == "" && user.ID > 0 && !user.Bot {
				var emails []gitLabUserEmail
				if _, err := c.requestJSON(ctx, http.MethodGet, "/users/"+strconv.Itoa(user.ID)+"/emails", nil, &emails); err != nil {
					return nil, err
				}
				user.Email = firstUsableEmail(emails)
			}
			identity := domain.Identity{
				Provider:   domain.ProviderGitLab,
				ProviderID: strconv.Itoa(user.ID),
				Username:   strings.ToLower(strings.TrimSpace(user.Username)),
				Email:      usableUserEmail(user.Email),
				Name:       strings.TrimSpace(user.Name),
			}
			users = append(users, ports.UserRecord{
				Identity: identity,
				Active:   strings.EqualFold(strings.TrimSpace(user.State), "active") && !user.Locked && !user.External && !user.Bot,
			})
		}
		if nextPage == "" || len(pageUsers) == 0 {
			break
		}
	}
	return users, nil
}

func (c *HookController) EnrichEvent(ctx context.Context, event domain.CanonicalEvent) (domain.CanonicalEvent, error) {
	cache := make(map[string]userLookupResult)
	var lookupErrors []error
	for _, identity := range eventIdentityPointers(&event) {
		if identity == nil || usableUserEmail(identity.Email) != "" {
			continue
		}
		key := userLookupKey(*identity)
		if key == "" {
			continue
		}
		result, ok := cache[key]
		if !ok {
			lookedUp, found, err := c.lookupIdentity(ctx, *identity)
			if err != nil {
				lookupErrors = append(lookupErrors, fmt.Errorf("lookup GitLab identity %s: %w", key, err))
				continue
			}
			result = userLookupResult{identity: lookedUp, found: found}
			cache[key] = result
		}
		if result.found {
			*identity = mergeIdentity(*identity, result.identity)
		}
	}
	return event, errors.Join(lookupErrors...)
}

type userLookupResult struct {
	identity domain.Identity
	found    bool
}

func (c *HookController) lookupIdentity(ctx context.Context, identity domain.Identity) (domain.Identity, bool, error) {
	if identity.Provider != "" && identity.Provider != domain.ProviderGitLab {
		return identity, false, nil
	}
	var user gitLabUser
	switch {
	case strings.TrimSpace(identity.ProviderID) != "":
		path := "/users/" + url.PathEscape(strings.TrimSpace(identity.ProviderID))
		if _, err := c.requestJSON(ctx, http.MethodGet, path, nil, &user); err != nil {
			if isNotFoundError(err) {
				return identity, false, nil
			}
			return identity, false, err
		}
	case strings.TrimSpace(identity.Username) != "":
		path := "/users?username=" + url.QueryEscape(strings.TrimSpace(identity.Username)) + "&per_page=2"
		var matches []gitLabUser
		if _, err := c.requestJSON(ctx, http.MethodGet, path, nil, &matches); err != nil {
			return identity, false, err
		}
		if len(matches) == 0 {
			return identity, false, nil
		}
		user = matches[0]
	default:
		return identity, false, nil
	}
	if user.ID <= 0 {
		return identity, false, nil
	}
	lookedUp := domain.Identity{
		Provider:   domain.ProviderGitLab,
		ProviderID: strconv.Itoa(user.ID),
		Username:   strings.ToLower(strings.TrimSpace(user.Username)),
		Email:      usableUserEmail(user.Email),
		Name:       strings.TrimSpace(user.Name),
	}
	if lookedUp.Email == "" && !user.Bot {
		var emails []gitLabUserEmail
		if _, err := c.requestJSON(ctx, http.MethodGet, "/users/"+strconv.Itoa(user.ID)+"/emails", nil, &emails); err != nil {
			return identity, false, err
		}
		lookedUp.Email = firstUsableEmail(emails)
	}
	return lookedUp, true, nil
}

func isNotFoundError(err error) bool {
	var apiErr *apiError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

func mergeIdentity(current, lookedUp domain.Identity) domain.Identity {
	if current.Provider == "" {
		current.Provider = lookedUp.Provider
	}
	if current.ProviderID == "" {
		current.ProviderID = lookedUp.ProviderID
	}
	if current.Username == "" {
		current.Username = lookedUp.Username
	}
	if usableUserEmail(current.Email) == "" {
		current.Email = lookedUp.Email
	}
	if current.Name == "" {
		current.Name = lookedUp.Name
	}
	return current
}

func userLookupKey(identity domain.Identity) string {
	if identity.ProviderID != "" {
		return "id:" + string(domain.ProviderGitLab) + ":" + strings.TrimSpace(identity.ProviderID)
	}
	if identity.Username != "" {
		return "username:" + string(domain.ProviderGitLab) + ":" + strings.ToLower(strings.TrimSpace(identity.Username))
	}
	return ""
}

func eventIdentityPointers(event *domain.CanonicalEvent) []*domain.Identity {
	identities := make([]*domain.Identity, 0, 16)
	identities = append(identities, &event.Actor)
	if event.Author != nil {
		identities = append(identities, event.Author)
	}
	for index := range event.Reviewers {
		identities = append(identities, &event.Reviewers[index])
	}
	for index := range event.Assignees {
		identities = append(identities, &event.Assignees[index])
	}
	for index := range event.Mentions {
		identities = append(identities, &event.Mentions[index])
	}
	if event.Pipeline != nil {
		identities = append(identities, &event.Pipeline.CommitAuthor)
	}
	if event.MergeRequest != nil {
		identities = append(identities, &event.MergeRequest.Author)
		for index := range event.MergeRequest.Reviewers {
			identities = append(identities, &event.MergeRequest.Reviewers[index])
		}
		for index := range event.MergeRequest.Assignees {
			identities = append(identities, &event.MergeRequest.Assignees[index])
		}
	}
	if event.Note != nil {
		if event.Note.MergeRequest != nil {
			identities = append(identities, &event.Note.MergeRequest.Author)
			for index := range event.Note.MergeRequest.Reviewers {
				identities = append(identities, &event.Note.MergeRequest.Reviewers[index])
			}
			for index := range event.Note.MergeRequest.Assignees {
				identities = append(identities, &event.Note.MergeRequest.Assignees[index])
			}
		}
		if event.Note.Issue != nil {
			identities = append(identities, &event.Note.Issue.Author)
			for index := range event.Note.Issue.Assignees {
				identities = append(identities, &event.Note.Issue.Assignees[index])
			}
		}
	}
	if event.Issue != nil {
		identities = append(identities, &event.Issue.Author)
		for index := range event.Issue.Assignees {
			identities = append(identities, &event.Issue.Assignees[index])
		}
	}
	return identities
}

func firstUsableEmail(emails []gitLabUserEmail) string {
	for _, item := range emails {
		if strings.TrimSpace(item.ConfirmedAt) != "" {
			if email := usableUserEmail(item.Email); email != "" {
				return email
			}
		}
	}
	for _, item := range emails {
		if email := usableUserEmail(item.Email); email != "" {
			return email
		}
	}
	return ""
}

func usableUserEmail(value string) string {
	value = strings.TrimSpace(value)
	address, err := mail.ParseAddress(value)
	if err != nil || address.Address != value || !strings.Contains(address.Address, "@") {
		return ""
	}
	return strings.ToLower(address.Address)
}
