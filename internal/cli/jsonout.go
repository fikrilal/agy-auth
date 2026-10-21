package cli

const jsonSchemaVersion = 1

type Envelope struct {
	SchemaVersion int       `json:"schema_version"`
	Command       string    `json:"command"`
	Data          any       `json:"data,omitempty"`
	Error         *ErrorDoc `json:"error,omitempty"`
}

type ErrorDoc struct {
	Code       string      `json:"code"`
	Message    string      `json:"message"`
	Hint       string      `json:"hint,omitempty"`
	Candidates []Candidate `json:"candidates,omitempty"`
}

type Candidate struct {
	ID    string `json:"id"`
	Email string `json:"email,omitempty"`
	Alias string `json:"alias,omitempty"`
}
