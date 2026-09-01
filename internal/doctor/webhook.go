package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"vitals/internal/diag"
)

const webhookTimeout = 5 * time.Second

// shouldNotify reports whether a report is worth paging someone about — a
// healthy run has nothing to say and would just be noise.
func shouldNotify(r diag.Report) bool {
	return r.Worst() != diag.OK
}

// maybeNotify POSTs the JSON envelope for (s, r) to url as
// application/json, but only when both a URL is configured and the report
// actually needs attention — a webhook that fires on every healthy run
// trains its recipient to ignore it.
func maybeNotify(url string, s Snapshot, r diag.Report) error {
	if url == "" || !shouldNotify(r) {
		return nil
	}
	body, err := json.Marshal(JSONReport(s, r))
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), webhookTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook %s returned %s", url, resp.Status)
	}
	return nil
}
