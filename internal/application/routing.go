package application

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"sort"
	"strings"
	"time"

	"github.com/dhkimxx/crowsnest/internal/domain"
	"github.com/dhkimxx/crowsnest/internal/ports"
)

const (
	ReasonCIFailed          = "ci_failed"
	ReasonCIRecovered       = "ci_recovered"
	ReasonMRReviewRequested = "mr_review_requested"
	ReasonMRAssigned        = "mr_assigned"
	ReasonMRUpdated         = "mr_updated"
	ReasonMRApproved        = "mr_approved"
	ReasonMRStateChanged    = "mr_state_changed"
	ReasonMRComment         = "mr_comment"
	ReasonMention           = "mention"
	ReasonIssueAssigned     = "issue_assigned"
	ReasonIssueUpdated      = "issue_updated"
)

type RouteResult struct {
	Deliveries      []domain.Delivery
	PipelineState   *domain.PipelineState
	UnresolvedUsers []domain.Identity
}

type Router struct {
	identities          ports.IdentityStore
	preferences         ports.PreferenceStore
	pipelineStates      ports.PipelineStateStore
	allowedEmailDomains map[string]struct{}
	recipientPolicy     *RecipientPolicy
	clock               func() time.Time
}

func NewRouter(identities ports.IdentityStore, preferences ports.PreferenceStore, pipelineStates ports.PipelineStateStore, allowedEmailDomains []string) *Router {
	return NewRouterWithRecipientPolicy(identities, preferences, pipelineStates, allowedEmailDomains, nil)
}

func NewRouterWithRecipientPolicy(identities ports.IdentityStore, preferences ports.PreferenceStore, pipelineStates ports.PipelineStateStore, allowedEmailDomains []string, recipientPolicy *RecipientPolicy) *Router {
	domains := make(map[string]struct{}, len(allowedEmailDomains))
	for _, domainName := range allowedEmailDomains {
		domainName = strings.ToLower(strings.TrimSpace(domainName))
		if domainName != "" {
			domains[domainName] = struct{}{}
		}
	}
	return &Router{
		identities:          identities,
		preferences:         preferences,
		pipelineStates:      pipelineStates,
		allowedEmailDomains: domains,
		recipientPolicy:     recipientPolicy,
		clock:               time.Now,
	}
}

func (r *Router) Route(ctx context.Context, event domain.CanonicalEvent) (RouteResult, error) {
	if event.EventKey == "" {
		return RouteResult{}, errors.New("event key is required")
	}
	candidates := newCandidateSet(event.Actor)

	switch event.Kind {
	case domain.EventKindPipeline:
		if err := r.routePipeline(ctx, &candidates, event); err != nil {
			return RouteResult{}, err
		}
	case domain.EventKindMergeRequest:
		r.routeMergeRequest(&candidates, event)
	case domain.EventKindNote:
		r.routeNote(&candidates, event)
	case domain.EventKindIssue:
		r.routeIssue(&candidates, event)
	default:
		return RouteResult{}, ports.ErrIgnoredEvent
	}

	resolved, unresolved, err := r.resolveCandidates(ctx, candidates, event)
	if err != nil {
		return RouteResult{}, err
	}
	result := RouteResult{UnresolvedUsers: unresolved}
	for _, item := range resolved {
		if len(item.reasons) == 0 || item.address.Value == "" {
			continue
		}
		if !r.recipientPolicy.Allows(item.address) {
			continue
		}
		reasonCodes := make([]string, 0, len(item.reasons))
		reasons := make([]domain.NotificationReason, 0, len(item.reasons))
		for code, text := range item.reasons {
			reasonCodes = append(reasonCodes, code)
			reasons = append(reasons, domain.NotificationReason{Code: code, Text: text})
		}
		sort.Strings(reasonCodes)
		sort.Slice(reasons, func(i, j int) bool { return reasons[i].Code < reasons[j].Code })
		reasonGroup := strings.Join(reasonCodes, "+")
		delivery := domain.Delivery{
			Key: deliveryKey(event, item.address, reasonGroup),
			Notification: domain.Notification{
				EventKey:   event.EventKey,
				Kind:       event.Kind,
				Action:     event.Action,
				Project:    event.Project,
				Object:     event.Object,
				Recipient:  item.address,
				Reasons:    reasons,
				Title:      notificationTitle(event, reasons),
				Summary:    notificationSummary(event),
				SourceText: event.SourceText,
				URL:        event.Object.URL,
				Facts:      notificationFacts(event),
				FailedJobs: failedJobNames(event),
			},
		}
		result.Deliveries = append(result.Deliveries, delivery)
	}

	if event.Kind == domain.EventKindPipeline && event.Pipeline != nil && r.pipelineStates != nil {
		state := pipelineStateFor(event, result.Deliveries, r.clock())
		if state != nil {
			result.PipelineState = state
		}
	}
	return result, nil
}

func (r *Router) routePipeline(ctx context.Context, candidates *candidateSet, event domain.CanonicalEvent) error {
	if event.Pipeline == nil {
		return nil
	}
	switch event.Pipeline.Status {
	case "failed":
		identity := event.Pipeline.CommitAuthor
		if !resolvableIdentity(identity) {
			identity = event.Actor
		}
		candidates.add(identity, ReasonCIFailed, "작성한 Commit의 Pipeline이 실패했습니다.", true)
	case "success":
		if r.pipelineStates == nil {
			return nil
		}
		state, err := r.pipelineStates.Get(ctx, pipelineCorrelationKey(event))
		if err != nil {
			return err
		}
		if state == nil || state.Status != "failed" {
			return nil
		}
		for _, recipient := range state.Recipients {
			candidates.addAddress(recipient, ReasonCIRecovered, "실패했던 Pipeline이 복구되었습니다.", true)
		}
	}
	return nil
}

func (r *Router) routeMergeRequest(candidates *candidateSet, event domain.CanonicalEvent) {
	mergeRequest := event.MergeRequest
	if mergeRequest == nil {
		return
	}
	switch event.Action {
	case "open":
		for _, reviewer := range mergeRequest.Reviewers {
			candidates.add(reviewer, ReasonMRReviewRequested, "Reviewer로 지정되었습니다.", false)
		}
		for _, assignee := range mergeRequest.Assignees {
			candidates.add(assignee, ReasonMRAssigned, "Merge Request의 Assignee로 지정되었습니다.", false)
		}
	case "update":
		for _, change := range event.Changes {
			switch change.Field {
			case "reviewers":
				for _, reviewer := range change.Added {
					candidates.add(reviewer, ReasonMRReviewRequested, "Reviewer로 지정되었습니다.", false)
				}
			case "assignees":
				for _, assignee := range change.Added {
					candidates.add(assignee, ReasonMRAssigned, "Merge Request의 Assignee로 지정되었습니다.", false)
				}
			case "title", "description", "source_branch", "target_branch", "draft":
				if hasIdentity(mergeRequest.Author) {
					candidates.add(mergeRequest.Author, ReasonMRUpdated, "Merge Request가 변경되었습니다.", false)
				}
				if change.Field == "draft" || change.Field == "target_branch" {
					for _, reviewer := range mergeRequest.Reviewers {
						candidates.add(reviewer, ReasonMRStateChanged, "검토 중인 Merge Request의 상태가 변경되었습니다.", false)
					}
				}
			}
		}
	case "approved", "approval", "unapproved", "unapproval":
		if hasIdentity(mergeRequest.Author) {
			candidates.add(mergeRequest.Author, ReasonMRApproved, "Merge Request의 Approval 상태가 변경되었습니다.", false)
		}
	case "merge", "close", "reopen":
		if hasIdentity(mergeRequest.Author) {
			candidates.add(mergeRequest.Author, ReasonMRStateChanged, mergeRequestStateReason(event.Action), false)
		}
	}
	for _, mention := range event.Mentions {
		candidates.add(mention, ReasonMention, "Merge Request에서 Mention되었습니다.", false)
	}
}

func (r *Router) routeNote(candidates *candidateSet, event domain.CanonicalEvent) {
	if event.Note == nil {
		return
	}
	if event.Note.System {
		return
	}
	if event.Note.MergeRequest != nil {
		if hasIdentity(event.Note.MergeRequest.Author) && !sameIdentity(event.Actor, event.Note.MergeRequest.Author) {
			candidates.add(event.Note.MergeRequest.Author, ReasonMRComment, "Merge Request에 새 Comment가 등록되었습니다.", false)
		}
	}
	if event.Note.Issue != nil {
		if hasIdentity(event.Note.Issue.Author) && !sameIdentity(event.Actor, event.Note.Issue.Author) {
			candidates.add(event.Note.Issue.Author, ReasonIssueUpdated, "Issue에 새 Comment가 등록되었습니다.", false)
		}
	}
	for _, mention := range event.Mentions {
		candidates.add(mention, ReasonMention, "Comment에서 Mention되었습니다.", false)
	}
}

func (r *Router) routeIssue(candidates *candidateSet, event domain.CanonicalEvent) {
	if event.Issue == nil {
		return
	}
	switch event.Action {
	case "open":
		for _, assignee := range event.Issue.Assignees {
			candidates.add(assignee, ReasonIssueAssigned, "Issue의 Assignee로 지정되었습니다.", false)
		}
	case "update":
		for _, change := range event.Changes {
			if change.Field == "assignees" {
				for _, assignee := range change.Added {
					candidates.add(assignee, ReasonIssueAssigned, "Issue 담당자로 지정되었습니다.", false)
				}
			} else if change.Field == "title" || change.Field == "description" || change.Field == "state" {
				if hasIdentity(event.Issue.Author) {
					candidates.add(event.Issue.Author, ReasonIssueUpdated, "Issue가 변경되었습니다.", false)
				}
			}
		}
	}
	for _, mention := range event.Mentions {
		candidates.add(mention, ReasonMention, "Issue에서 Mention되었습니다.", false)
	}
}

func (r *Router) resolveCandidates(ctx context.Context, candidates candidateSet, event domain.CanonicalEvent) ([]resolvedCandidate, []domain.Identity, error) {
	identities := make([]domain.Identity, 0, len(candidates.items))
	keys := make([]string, 0, len(candidates.items))
	for key, item := range candidates.items {
		keys = append(keys, key)
		identities = append(identities, item.identity)
	}
	resolvedIdentities := identities
	if r.identities != nil {
		var err error
		resolvedIdentities, err = r.identities.Resolve(ctx, identities)
		if err != nil {
			return nil, nil, err
		}
	}
	resolved := make(map[string]resolvedCandidate, len(resolvedIdentities))
	resolvedOriginalKeys := make(map[string]struct{}, len(resolvedIdentities))
	for _, identity := range resolvedIdentities {
		address, ok := emailAddress(identity)
		if !ok || !r.allowed(address.Value) {
			continue
		}
		key := candidateKey(identity)
		item, exists := candidates.items[key]
		if !exists {
			for _, originalKey := range keys {
				if sameIdentity(candidates.items[originalKey].identity, identity) {
					item = candidates.items[originalKey]
					exists = true
					break
				}
			}
		}
		if !exists {
			continue
		}
		resolvedOriginalKeys[key] = struct{}{}
		if !item.allowSelf && sameIdentity(event.Actor, identity) {
			continue
		}
		if r.preferences != nil {
			filteredReasons := make(map[string]string, len(item.reasons))
			for code, text := range item.reasons {
				enabled, err := r.preferences.Enabled(ctx, address, event.Kind, code, event.Project.Path)
				if err != nil {
					return nil, nil, err
				}
				if enabled {
					filteredReasons[code] = text
				}
			}
			item.reasons = filteredReasons
		}
		if existing, ok := resolved[address.Value]; ok {
			for code, text := range item.reasons {
				existing.reasons[code] = text
			}
			resolved[address.Value] = existing
		} else {
			resolved[address.Value] = resolvedCandidate{identity: identity, address: address, reasons: item.reasons}
		}
	}

	var unresolved []domain.Identity
	for key, identity := range candidates.items {
		if _, ok := resolvedOriginalKeys[key]; ok {
			continue
		}
		if _, ok := emailAddress(identity.identity); !ok {
			unresolved = append(unresolved, identity.identity)
		}
	}
	result := make([]resolvedCandidate, 0, len(resolved))
	for _, item := range resolved {
		result = append(result, item)
	}
	return result, unresolved, nil
}

func (r *Router) allowed(address string) bool {
	if len(r.allowedEmailDomains) == 0 {
		return true
	}
	parts := strings.SplitN(address, "@", 2)
	if len(parts) != 2 {
		return false
	}
	_, ok := r.allowedEmailDomains[strings.ToLower(parts[1])]
	return ok
}

type candidate struct {
	identity  domain.Identity
	address   domain.RecipientAddress
	reasons   map[string]string
	allowSelf bool
}

type resolvedCandidate struct {
	identity domain.Identity
	address  domain.RecipientAddress
	reasons  map[string]string
}

type candidateSet struct {
	actor domain.Identity
	items map[string]candidate
}

func newCandidateSet(actor domain.Identity) candidateSet {
	return candidateSet{actor: actor, items: make(map[string]candidate)}
}

func (s *candidateSet) add(identity domain.Identity, code, text string, allowSelf bool) {
	if !hasIdentity(identity) {
		return
	}
	key := candidateKey(identity)
	item := s.items[key]
	if item.reasons == nil {
		item.identity = identity
		item.reasons = make(map[string]string)
	}
	item.reasons[code] = text
	item.allowSelf = item.allowSelf || allowSelf
	s.items[key] = item
}

func (s *candidateSet) addAddress(address domain.RecipientAddress, code, text string, allowSelf bool) {
	if address.Kind == "" || address.Value == "" {
		return
	}
	identity := domain.Identity{Email: strings.ToLower(strings.TrimSpace(address.Value))}
	key := candidateKey(identity)
	item := s.items[key]
	if item.reasons == nil {
		item.identity = identity
		item.address = address
		item.reasons = make(map[string]string)
	}
	item.reasons[code] = text
	item.allowSelf = item.allowSelf || allowSelf
	s.items[key] = item
}

func candidateKey(identity domain.Identity) string {
	if identity.ProviderID != "" {
		return "id:" + string(identity.Provider) + ":" + identity.ProviderID
	}
	if identity.Username != "" {
		return "username:" + string(identity.Provider) + ":" + strings.ToLower(identity.Username)
	}
	return "email:" + strings.ToLower(identity.Email)
}

func emailAddress(identity domain.Identity) (domain.RecipientAddress, bool) {
	email := strings.ToLower(strings.TrimSpace(identity.Email))
	parsed, err := mail.ParseAddress(email)
	if email == "" || strings.Contains(email, "noreply") || email == "[redacted]" || err != nil || parsed.Address != email {
		return domain.RecipientAddress{}, false
	}
	return domain.RecipientAddress{Kind: domain.AddressKindEmail, Value: email, DisplayName: identity.Name}, true
}

func hasIdentity(identity domain.Identity) bool {
	return identity.ProviderID != "" || identity.Username != "" || identity.Email != "" || identity.Name != ""
}

func resolvableIdentity(identity domain.Identity) bool {
	return identity.ProviderID != "" || identity.Username != "" || identity.Email != ""
}

func sameIdentity(left, right domain.Identity) bool {
	if left.ProviderID != "" && right.ProviderID != "" && left.Provider == right.Provider {
		return left.ProviderID == right.ProviderID
	}
	if left.Email != "" && right.Email != "" {
		return strings.EqualFold(left.Email, right.Email)
	}
	return left.Username != "" && right.Username != "" && left.Provider == right.Provider && strings.EqualFold(left.Username, right.Username)
}

func pipelineCorrelationKey(event domain.CanonicalEvent) string {
	project := event.Project.ID
	if project == "" {
		project = event.Project.Path
	}
	if event.Pipeline != nil && event.Pipeline.MergeRequest != nil && event.Pipeline.MergeRequest.IID != "" {
		return fmt.Sprintf("gitlab:%s:mr:%s", project, event.Pipeline.MergeRequest.IID)
	}
	ref := ""
	if event.Pipeline != nil {
		ref = event.Pipeline.Ref
	}
	return fmt.Sprintf("gitlab:%s:ref:%s", project, ref)
}

func pipelineStateFor(event domain.CanonicalEvent, deliveries []domain.Delivery, now time.Time) *domain.PipelineState {
	if event.Pipeline == nil || (event.Pipeline.Status != "failed" && event.Pipeline.Status != "success") {
		return nil
	}
	recipients := make([]domain.RecipientAddress, 0, len(deliveries))
	seen := map[string]struct{}{}
	for _, delivery := range deliveries {
		if !strings.Contains(delivery.Key, ReasonCIFailed) && event.Pipeline.Status == "failed" {
			continue
		}
		address := delivery.Notification.Recipient
		if _, ok := seen[address.Value]; ok {
			continue
		}
		seen[address.Value] = struct{}{}
		recipients = append(recipients, address)
	}
	return &domain.PipelineState{CorrelationKey: pipelineCorrelationKey(event), Status: event.Pipeline.Status, Recipients: recipients, UpdatedAt: now.UTC()}
}

func deliveryKey(event domain.CanonicalEvent, address domain.RecipientAddress, reasonGroup string) string {
	base := event.EventKey
	if event.Kind == domain.EventKindNote && event.Note != nil && event.Note.ID != "" {
		base = fmt.Sprintf("gitlab:note:%s:%s", event.Project.ID, event.Note.ID)
	}
	if strings.Contains(reasonGroup, ReasonMention) && event.Object.IID != "" {
		switch event.Kind {
		case domain.EventKindMergeRequest:
			base = fmt.Sprintf("gitlab:merge_request:%s:%s:mention", event.Project.ID, event.Object.IID)
		case domain.EventKindIssue:
			base = fmt.Sprintf("gitlab:issue:%s:%s:mention", event.Project.ID, event.Object.IID)
		}
	}
	return base + ":" + strings.ToLower(address.Value) + ":" + reasonGroup
}

func notificationTitle(event domain.CanonicalEvent, reasons []domain.NotificationReason) string {
	if len(reasons) == 1 {
		return notificationReasonTitle(event, reasons[0].Code)
	}
	if event.Kind == domain.EventKindPipeline {
		return "Pipeline update"
	}
	if event.Kind == domain.EventKindMergeRequest {
		return "Merge Request update"
	}
	if event.Kind == domain.EventKindIssue {
		return "Issue update"
	}
	return "GitLab notification"
}

func notificationSummary(event domain.CanonicalEvent) string {
	project := event.Project.Path
	if project == "" {
		project = event.Project.ID
	}
	object := resourceLabel(event)
	parts := []string{project}
	if object != "" {
		parts = append(parts, object)
	}
	if action := eventActionLabel(event); action != "" {
		parts = append(parts, action)
	}
	return strings.Join(parts, " · ")
}

func notificationFacts(event domain.CanonicalEvent) map[string]string {
	facts := make(map[string]string)
	if event.Project.Path != "" {
		facts["Project"] = event.Project.Path
	}
	if event.Object.Title != "" {
		facts["Title"] = event.Object.Title
	}
	if event.Actor.Name != "" {
		facts[actorFactLabel(event)] = event.Actor.Name
	} else if event.Actor.Username != "" {
		facts[actorFactLabel(event)] = "@" + event.Actor.Username
	}
	if action := eventActionLabel(event); action != "" {
		facts["Status"] = action
	}
	if event.Pipeline != nil {
		if event.Pipeline.Ref != "" {
			facts["Branch"] = event.Pipeline.Ref
		}
		if event.Pipeline.Source != "" {
			facts["Source"] = strings.ToUpper(event.Pipeline.Source[:1]) + event.Pipeline.Source[1:]
		}
	}
	if event.MergeRequest != nil && (event.MergeRequest.SourceBranch != "" || event.MergeRequest.TargetBranch != "") {
		facts["Branch"] = event.MergeRequest.SourceBranch + " → " + event.MergeRequest.TargetBranch
	}
	if event.Issue != nil && event.Issue.State != "" {
		facts["State"] = event.Issue.State
	}
	return facts
}

func notificationReasonTitle(event domain.CanonicalEvent, reasonCode string) string {
	switch reasonCode {
	case ReasonCIFailed:
		return "Pipeline failed"
	case ReasonCIRecovered:
		return "Pipeline recovered"
	case ReasonMRReviewRequested:
		return "Review requested"
	case ReasonMRAssigned:
		return "Assignee assigned"
	case ReasonMRUpdated:
		return "Merge Request updated"
	case ReasonMRApproved:
		return "Approval changed"
	case ReasonMRStateChanged:
		switch event.Action {
		case "merge":
			return "Merge Request merged"
		case "close":
			return "Merge Request closed"
		case "reopen":
			return "Merge Request reopened"
		default:
			return "Merge Request state changed"
		}
	case ReasonMRComment:
		return "New Comment"
	case ReasonMention:
		return "Mentioned"
	case ReasonIssueAssigned:
		return "Issue assigned"
	case ReasonIssueUpdated:
		if event.Note != nil {
			return "New Comment"
		}
		return "Issue updated"
	default:
		return "GitLab notification"
	}
}

func mergeRequestStateReason(action string) string {
	switch action {
	case "merge":
		return "Merge Request가 머지되었습니다."
	case "close":
		return "Merge Request가 닫혔습니다."
	case "reopen":
		return "Merge Request가 다시 열렸습니다."
	default:
		return "Merge Request의 상태가 변경되었습니다."
	}
}

func eventActionLabel(event domain.CanonicalEvent) string {
	if event.Kind == domain.EventKindPipeline && event.Pipeline != nil {
		switch event.Pipeline.Status {
		case "failed":
			return "Failed"
		case "success":
			return "Recovered"
		}
	}
	switch event.Action {
	case "open":
		return "Opened"
	case "update":
		return "Updated"
	case "approved", "approval":
		return "Approved"
	case "unapproved", "unapproval":
		return "Approval removed"
	case "merge":
		return "Merged"
	case "close":
		return "Closed"
	case "reopen":
		return "Reopened"
	case "create":
		return "Created"
	default:
		return event.Action
	}
}

func resourceLabel(event domain.CanonicalEvent) string {
	if event.Object.IID != "" {
		switch event.Object.Kind {
		case domain.EventKindMergeRequest:
			return "!" + event.Object.IID
		case domain.EventKindIssue:
			return "#" + event.Object.IID
		case domain.EventKindPipeline:
			return "Pipeline #" + event.Object.IID
		}
		return event.Object.IID
	}
	if event.Object.Title != "" {
		return event.Object.Title
	}
	return event.Object.ID
}

func actorFactLabel(event domain.CanonicalEvent) string {
	switch event.Kind {
	case domain.EventKindPipeline:
		return "Triggered by"
	case domain.EventKindNote:
		return "Commented by"
	default:
		return "Changed by"
	}
}

func failedJobNames(event domain.CanonicalEvent) []string {
	if event.Pipeline == nil {
		return nil
	}
	names := make([]string, 0, len(event.Pipeline.FailedJobs))
	for _, job := range event.Pipeline.FailedJobs {
		if job.Name != "" {
			names = append(names, job.Name)
		}
	}
	return names
}
