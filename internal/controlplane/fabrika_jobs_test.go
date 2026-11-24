package controlplane

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/owainlewis/machinist/internal/sources"
	"github.com/owainlewis/machinist/internal/tickets"
)

func TestFabrikaJobReadsStageAndFallbacks(t *testing.T) {
	root := t.TempDir()
	s := Server{config: testConfig()}
	s.config.Server.Database = filepath.Join(root, "tickets.db")
	ticket := tickets.Ticket{ID: "t_0123456789abcdef", Agent: "fabrika"}
	for dir, body := range map[string]string{
		"model-stages":    "review\n",
		"model-fallbacks": "unrelated text\n▸ Fallback review-code: a → b\n",
	} {
		path := filepath.Join(root, dir)
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, ticket.ID), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s.addFabrikaStatus(&ticket)
	if ticket.FabrikaStage != "review" || !reflect.DeepEqual(ticket.FabrikaFallbacks, []string{"review-code: a → b"}) {
		t.Fatalf("stage = %s, fallbacks = %v", ticket.FabrikaStage, ticket.FabrikaFallbacks)
	}
}

func TestGitHubIntakeCanExcludeAConnectedProject(t *testing.T) {
	config := testConfig()
	config.GitHub.Repositories = []string{"OWNER/Toy", "owner/nymo"}
	config.GitHub.Issues.Label = "fabrika"
	config.GitHub.Issues.Repositories = []string{"owner/toy"}
	poller, err := githubPoller(config, testFactory(), nil, nil, io.Discard, func(context.Context, string) (string, error) {
		return "fixture", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	source := poller.Tracker.(*sources.GitHubIssues)
	if source.Label != "fabrika" || !reflect.DeepEqual(source.Repositories, []string{"owner/toy"}) {
		t.Fatalf("label = %s, repositories = %v", source.Label, source.Repositories)
	}
}
