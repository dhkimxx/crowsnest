package domain

type IdentitySyncReport struct {
	DryRun             bool `json:"dry_run"`
	GitLabUsers        int  `json:"gitlab_users"`
	EligibleUsers      int  `json:"eligible_users"`
	FeishuUsersFound   int  `json:"feishu_users_found"`
	MappingsConsidered int  `json:"mappings_considered"`
	MappingsUpserted   int  `json:"mappings_upserted"`
	MappingsEnabled    int  `json:"mappings_enabled"`
	MappingsDisabled   int  `json:"mappings_disabled"`
	SkippedNoEmail     int  `json:"skipped_no_email"`
	SkippedDomain      int  `json:"skipped_domain"`
	SkippedInactive    int  `json:"skipped_inactive"`
}
