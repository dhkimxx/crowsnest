package domain

type AIEvidence struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

type AIAnnotation struct {
	Importance        string       `json:"importance"`
	Summary           string       `json:"summary"`
	Impact            string       `json:"impact,omitempty"`
	RecommendedAction string       `json:"recommended_action,omitempty"`
	Confidence        float64      `json:"confidence,omitempty"`
	Evidence          []AIEvidence `json:"evidence,omitempty"`
	Model             string       `json:"model,omitempty"`
}
