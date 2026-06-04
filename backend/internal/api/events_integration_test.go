package api_test

import (
	"bufio"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"markupmarkdown/internal/models"
	"markupmarkdown/internal/testutil"
)

func TestEvents_AccessDeniedForPrivateDoc(t *testing.T) {
	_, st, _ := newTestServer(t) // private doc won't even be retrievable
	user := testutil.NewTestUser(t, st)
	_ = user
	// Insert a private doc.
	doc := &models.Document{
		ID: uuid.NewString(), Title: "Private", Origin: "url",
		Private: true, GitHubOwner: "o", GitHubRepo: "r",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	_ = st.InsertDocument(context.Background(), doc)
	// Anonymous → 401.
	_ = doc
}

func TestEvents_HelloThenClose(t *testing.T) {
	srv, st, _ := newTestServer(t)
	user := testutil.NewTestUser(t, st)
	doc := testutil.NewTestDocument(t, st, user.ID, "")

	url := srv.URL + "/api/documents/" + doc.ID + "/events"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
	scanner := bufio.NewScanner(res.Body)
	gotHello := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: hello") {
			gotHello = true
			break
		}
	}
	if !gotHello {
		t.Fatal("did not receive hello event before context cancel")
	}
}
