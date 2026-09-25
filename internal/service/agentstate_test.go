package service

import (
	"testing"

	"github.com/na0fu3y/ochakai/internal/config"
)

// The page reads BigQuery with the OAuth client and the billing project
// to seed from a schema, which asks no model, so stats carries both
// whether or not the agent is on (design doc 0148 §2.4).
func TestAgentStateCarriesTheClientWithoutTheAgent(t *testing.T) {
	s := &Service{Config: &config.Config{OAuthClientID: "1-abc.apps.googleusercontent.com", BigQueryProject: "billing"}}
	st := s.agentState(nil)
	if st.Enabled {
		t.Error("no model, yet the agent reads as enabled")
	}
	if st.OAuthClientID != "1-abc.apps.googleusercontent.com" || st.BigQueryProject != "billing" {
		t.Errorf("client = %q, project = %q; want both carried with the agent off", st.OAuthClientID, st.BigQueryProject)
	}
}
