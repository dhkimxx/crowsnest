package gitlabv176

import (
	"context"
	"net/http"
	"net/mail"
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
