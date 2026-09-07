package application

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"github.com/dhkimxx/crowsnest/internal/domain"
	"github.com/dhkimxx/crowsnest/internal/ports"
)

type IdentitySyncService struct {
	users          ports.UserDirectory
	emailDirectory ports.EmailDirectory
	identities     ports.IdentitySyncStore
	allowedDomains map[string]struct{}
}

func NewIdentitySyncService(users ports.UserDirectory, emailDirectory ports.EmailDirectory, identities ports.IdentitySyncStore, allowedEmailDomains []string) *IdentitySyncService {
	domains := make(map[string]struct{}, len(allowedEmailDomains))
	for _, domainName := range allowedEmailDomains {
		domainName = strings.ToLower(strings.TrimSpace(domainName))
		if domainName != "" {
			domains[domainName] = struct{}{}
		}
	}
	return &IdentitySyncService{users: users, emailDirectory: emailDirectory, identities: identities, allowedDomains: domains}
}

func (s *IdentitySyncService) Sync(ctx context.Context, dryRun bool) (domain.IdentitySyncReport, error) {
	report := domain.IdentitySyncReport{DryRun: dryRun}
	if s == nil || s.users == nil || s.identities == nil {
		return report, errors.New("identity sync is not configured")
	}
	if s.emailDirectory == nil {
		report.EmailVerification = "gitlab"
	} else {
		report.EmailVerification = "feishu"
	}
	users, err := s.users.ListUsers(ctx)
	if err != nil {
		return report, fmt.Errorf("list GitLab users: %w", err)
	}
	report.GitLabUsers = len(users)

	type candidate struct {
		identity domain.Identity
		active   bool
		email    string
	}
	candidates := make([]candidate, 0, len(users))
	lookupEmails := make([]string, 0, len(users))
	for _, user := range users {
		email := normalizeSyncEmail(user.Identity.Email)
		if email == "" {
			report.SkippedNoEmail++
			continue
		}
		if !s.allowed(email) {
			report.SkippedDomain++
			continue
		}
		report.EligibleUsers++
		identity := user.Identity
		identity.Provider = domain.ProviderGitLab
		identity.Email = email
		candidates = append(candidates, candidate{identity: identity, active: user.Active, email: email})
		if user.Active {
			lookupEmails = append(lookupEmails, email)
		} else {
			report.SkippedInactive++
		}
	}

	found := make(map[string]ports.DirectoryUser)
	if s.emailDirectory != nil {
		found, err = s.emailDirectory.LookupEmails(ctx, lookupEmails)
		if err != nil {
			return report, fmt.Errorf("lookup Feishu users by email: %w", err)
		}
	} else {
		for _, item := range candidates {
			if item.active {
				found[item.email] = ports.DirectoryUser{Email: item.email}
			}
		}
	}
	enabledProviderIDs := make([]string, 0, len(candidates))
	var syncErrors []error
	for _, item := range candidates {
		_, feishuFound := found[item.email]
		if item.active && feishuFound && s.emailDirectory != nil {
			report.FeishuUsersFound++
		}
		enabled := item.active && feishuFound
		report.MappingsConsidered++
		if dryRun {
			continue
		}
		if err := s.identities.Upsert(ctx, domain.ProviderGitLab, item.identity, enabled); err != nil {
			syncErrors = append(syncErrors, fmt.Errorf("upsert GitLab identity %s: %w", item.identity.ProviderID, err))
			continue
		}
		report.MappingsUpserted++
		if enabled {
			report.MappingsEnabled++
			if item.identity.ProviderID != "" {
				enabledProviderIDs = append(enabledProviderIDs, item.identity.ProviderID)
			}
		}
	}
	if !dryRun {
		disabled, disableErr := s.identities.DisableExcept(ctx, domain.ProviderGitLab, enabledProviderIDs)
		if disableErr != nil {
			syncErrors = append(syncErrors, disableErr)
		} else {
			report.MappingsDisabled = disabled
		}
	}
	return report, errors.Join(syncErrors...)
}

func (s *IdentitySyncService) allowed(email string) bool {
	if len(s.allowedDomains) == 0 {
		return true
	}
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return false
	}
	_, ok := s.allowedDomains[strings.ToLower(parts[1])]
	return ok
}

func normalizeSyncEmail(value string) string {
	value = strings.TrimSpace(value)
	address, err := mail.ParseAddress(value)
	if err != nil || address.Address != value || strings.Contains(strings.ToLower(address.Address), "noreply") {
		return ""
	}
	return strings.ToLower(address.Address)
}
