package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	openAIDailyTokenProfileURL       = "https://chatgpt.com/backend-api/wham/profiles/me"
	maximumOpenAIDailyTokenBodyBytes = 4 << 20
)

type OpenAIDailyTokenBucket struct {
	StartDate string
	Tokens    int64
}

// OpenAIDailyTokenHistory is an account-scoped OpenAI profile snapshot.
// OpenAI supplies the reporting date independently of the local read time.
type OpenAIDailyTokenHistory struct {
	AccountID      string
	ReadStartedAt  time.Time
	ReportingDate  time.Time
	GeneratedAt    time.Time
	LifetimeTokens int64
	Buckets        []OpenAIDailyTokenBucket
}

type OpenAIDailyTokenSource struct {
	credentials *savedLogin
	client      *http.Client
	now         func() time.Time
}

func NewOpenAIDailyTokenSource(home string, client *http.Client) *OpenAIDailyTokenSource {
	credentials := newSavedLogin(home, client)
	readOnlyClient := *credentials.client
	readOnlyClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &OpenAIDailyTokenSource{credentials: credentials, client: &readOnlyClient, now: time.Now}
}

func (s *OpenAIDailyTokenSource) Read(ctx context.Context) (OpenAIDailyTokenHistory, error) {
	if s == nil || s.credentials == nil {
		return OpenAIDailyTokenHistory{}, errors.New("OpenAI daily token source is unavailable")
	}
	readStarted := s.now().UTC()
	credentials, err := s.credentials.CurrentCredentials()
	if err != nil {
		return OpenAIDailyTokenHistory{}, err
	}
	accountID := strings.TrimSpace(credentials.AccountID)
	if accountID == "" {
		return OpenAIDailyTokenHistory{}, errors.New("OpenAI daily token source has no account identity")
	}

	response, err := s.fetch(ctx, credentials)
	if err != nil {
		return OpenAIDailyTokenHistory{}, err
	}
	if response.StatusCode == http.StatusUnauthorized {
		discardOpenAIDailyTokenBody(response.Body)
		return OpenAIDailyTokenHistory{}, signInRequired(errors.New("OpenAI rejected stored credentials"))
	}
	defer response.Body.Close()
	if response.StatusCode/100 != 2 {
		discardOpenAIDailyTokenBody(response.Body)
		return OpenAIDailyTokenHistory{}, fmt.Errorf("OpenAI daily tokens returned status %d", response.StatusCode)
	}
	encoded, err := io.ReadAll(io.LimitReader(response.Body, maximumOpenAIDailyTokenBodyBytes+1))
	if err != nil {
		return OpenAIDailyTokenHistory{}, fmt.Errorf("read OpenAI daily tokens: %w", err)
	}
	if len(encoded) > maximumOpenAIDailyTokenBodyBytes {
		return OpenAIDailyTokenHistory{}, errors.New("OpenAI daily tokens response is too large")
	}
	history, err := decodeOpenAIDailyTokenHistory(accountID, readStarted, encoded)
	if err != nil {
		return OpenAIDailyTokenHistory{}, err
	}
	return history, nil
}

func (s *OpenAIDailyTokenSource) fetch(ctx context.Context, credentials Credentials) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, openAIDailyTokenProfileURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+credentials.AccessToken)
	request.Header.Set("ChatGPT-Account-ID", credentials.AccountID)
	request.Header.Set("User-Agent", "codex-cli")
	response, err := s.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("read OpenAI daily tokens: %w", err)
	}
	return response, nil
}

func discardOpenAIDailyTokenBody(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 1<<20))
	_ = body.Close()
}

type openAIDailyTokenProfileResponse struct {
	Metadata *struct {
		StatsAsOf   string          `json:"stats_as_of"`
		GeneratedAt time.Time       `json:"generated_at"`
		StatsError  json.RawMessage `json:"stats_error"`
	} `json:"metadata"`
	Stats *struct {
		LifetimeTokens    *int64                        `json:"lifetime_tokens"`
		DailyUsageBuckets *[]openAIDailyTokenBucketWire `json:"daily_usage_buckets"`
	} `json:"stats"`
}

type openAIDailyTokenBucketWire struct {
	StartDate string `json:"start_date"`
	Tokens    *int64 `json:"tokens"`
}

func decodeOpenAIDailyTokenHistory(accountID string, readStarted time.Time, encoded []byte) (OpenAIDailyTokenHistory, error) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return OpenAIDailyTokenHistory{}, errors.New("OpenAI daily token history requires an account identity")
	}
	if readStarted.IsZero() {
		return OpenAIDailyTokenHistory{}, errors.New("OpenAI daily token history requires a read start time")
	}
	readStarted = readStarted.UTC()
	var wire openAIDailyTokenProfileResponse
	if err := json.Unmarshal(encoded, &wire); err != nil {
		return OpenAIDailyTokenHistory{}, fmt.Errorf("decode OpenAI daily tokens: %w", err)
	}
	if wire.Metadata == nil || wire.Metadata.GeneratedAt.IsZero() {
		return OpenAIDailyTokenHistory{}, errors.New("OpenAI daily tokens response has no reporting metadata")
	}
	reportingDate, err := time.Parse(time.DateOnly, wire.Metadata.StatsAsOf)
	if err != nil || reportingDate.Format(time.DateOnly) != wire.Metadata.StatsAsOf {
		return OpenAIDailyTokenHistory{}, errors.New("OpenAI daily tokens reporting date is invalid")
	}
	if statsError := strings.TrimSpace(string(wire.Metadata.StatsError)); statsError != "" && statsError != "null" {
		return OpenAIDailyTokenHistory{}, errors.New("OpenAI daily tokens profile reported a statistics error")
	}
	if wire.Stats == nil || wire.Stats.LifetimeTokens == nil || wire.Stats.DailyUsageBuckets == nil {
		return OpenAIDailyTokenHistory{}, errors.New("OpenAI daily tokens response is incomplete")
	}
	if *wire.Stats.LifetimeTokens < 0 {
		return OpenAIDailyTokenHistory{}, errors.New("OpenAI lifetime token total is invalid")
	}

	rawBuckets := *wire.Stats.DailyUsageBuckets
	buckets := make([]OpenAIDailyTokenBucket, 0, len(rawBuckets))
	seen := make(map[string]struct{}, len(rawBuckets))
	var bucketTotal int64
	for index, raw := range rawBuckets {
		startDate := raw.StartDate
		date, err := time.Parse(time.DateOnly, startDate)
		if err != nil || date.Format(time.DateOnly) != startDate {
			return OpenAIDailyTokenHistory{}, fmt.Errorf("OpenAI daily token bucket %d has an invalid date", index)
		}
		if date.After(reportingDate) {
			return OpenAIDailyTokenHistory{}, fmt.Errorf("OpenAI daily token bucket %d exceeds its reporting date", index)
		}
		if _, duplicate := seen[startDate]; duplicate {
			return OpenAIDailyTokenHistory{}, fmt.Errorf("OpenAI daily token bucket %d repeats a date", index)
		}
		seen[startDate] = struct{}{}
		if raw.Tokens == nil || *raw.Tokens < 0 {
			return OpenAIDailyTokenHistory{}, fmt.Errorf("OpenAI daily token bucket %d has an invalid total", index)
		}
		if bucketTotal > math.MaxInt64-*raw.Tokens {
			return OpenAIDailyTokenHistory{}, errors.New("OpenAI daily token bucket total overflows")
		}
		bucketTotal += *raw.Tokens
		buckets = append(buckets, OpenAIDailyTokenBucket{StartDate: startDate, Tokens: *raw.Tokens})
	}
	if bucketTotal != *wire.Stats.LifetimeTokens {
		return OpenAIDailyTokenHistory{}, errors.New("OpenAI daily token buckets do not match lifetime total")
	}
	sort.Slice(buckets, func(i, j int) bool { return buckets[i].StartDate < buckets[j].StartDate })
	return OpenAIDailyTokenHistory{
		AccountID: accountID, ReadStartedAt: readStarted,
		ReportingDate: reportingDate, GeneratedAt: wire.Metadata.GeneratedAt.UTC(),
		LifetimeTokens: *wire.Stats.LifetimeTokens, Buckets: buckets,
	}, nil
}
