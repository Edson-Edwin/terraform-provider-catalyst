// SPDX-License-Identifier: MPL-2.0
package catalyst

import (
	"net/http"
	"strings"
	"sync"
	"testing"
)

func TestSmartTrainMaterialConcurrentCreate(t *testing.T) {
	const materialURL = "https://example.com/guide"
	var mu sync.Mutex
	posts := 0
	client := testClient(t, Config{AccessToken: "fixture", OrganizationID: "321"}, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case http.MethodPost:
			posts++
			writeResponse(t, w, `{"status":"SUCCESS","code":"SUCCESS","info":{"files":[{"id":"55","url":"https://example.com/guide/"}]}}`)
		case http.MethodGet:
			if posts == 0 {
				writeResponse(t, w, `{"rag_training_data":[]}`)
				return
			}
			writeResponse(t, w, `{"rag_training_data":[{"id":"55","url":"https://example.com/guide/","title":"Guide","source":"URL"}]}`)
		default:
			t.Error("unexpected SmartTrain method")
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, err := client.CreateSmartTrainMaterial(t.Context(), "123", "456", materialURL, "Guide")
			results <- err
		}()
	}
	close(start)
	accepted := 0
	for range 2 {
		switch err := <-results; {
		case err == nil:
			accepted++
		case strings.Contains(err.Error(), "already uses this URL"):
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if accepted != 1 || posts != 1 {
		t.Fatalf("duplicate material creation: %d accepted operations, %d writes", accepted, posts)
	}
}
