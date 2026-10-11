package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"time"
)

const openAIDailyUsageBreakdownURL = "https://chatgpt.com/backend-api/wham/usage/daily-token-usage-breakdown"

// OpenAIAllowanceShare is the share of an account's allowance that one model
// at one speed used on one UTC day, in percent. OpenAI counts every client
// and machine, ChatGPT Work on the web included. A share is the work's credits
// at OpenAI's published Codex credit rates divided by the account's allowance.
type OpenAIAllowanceShare struct {
	Date    string
	Model   string
	Fast    bool
	Percent float64
}

// ReadDailyAllowanceShares reads the per-model, per-speed allowance shares for
// the UTC dates firstDate through lastDate. OpenAI fills in a day one to two
// days after it ends; until then the day's shares are zero.
func (s *OpenAIDailyTokenSource) ReadDailyAllowanceShares(ctx context.Context, firstDate, lastDate time.Time) (string, []OpenAIAllowanceShare, error) {
	query := url.Values{}
	query.Set("start_date", firstDate.UTC().Format(time.DateOnly))
	query.Set("end_date", lastDate.UTC().Format(time.DateOnly))
	query.Set("group_by", "day")
	accountID, encoded, err := s.get(ctx, openAIDailyUsageBreakdownURL+"?"+query.Encode())
	if err != nil {
		return "", nil, err
	}
	shares, err := decodeOpenAIAllowanceShares(encoded)
	return accountID, shares, err
}

func decodeOpenAIAllowanceShares(encoded []byte) ([]OpenAIAllowanceShare, error) {
	var wire struct {
		Units *string `json:"units"`
		Data  []struct {
			Date   string `json:"date"`
			Models []struct {
				Model   string   `json:"model"`
				Speed   string   `json:"speed"`
				Credits *float64 `json:"credits"`
			} `json:"models"`
		} `json:"data"`
	}
	if err := json.Unmarshal(encoded, &wire); err != nil {
		return nil, fmt.Errorf("decode OpenAI daily usage breakdown: %w", err)
	}
	if wire.Units == nil || *wire.Units != "percent" {
		return nil, errors.New("OpenAI daily usage breakdown is not in percent of the allowance")
	}
	var shares []OpenAIAllowanceShare
	for _, day := range wire.Data {
		if _, err := time.Parse(time.DateOnly, day.Date); err != nil {
			return nil, fmt.Errorf("OpenAI daily usage breakdown has an invalid date %q", day.Date)
		}
		for _, model := range day.Models {
			if model.Model == "" || model.Credits == nil || *model.Credits < 0 || math.IsInf(*model.Credits, 0) || math.IsNaN(*model.Credits) {
				return nil, fmt.Errorf("OpenAI daily usage breakdown for %s has an invalid model share", day.Date)
			}
			if model.Speed != "standard" && model.Speed != "fast" {
				return nil, fmt.Errorf("OpenAI daily usage breakdown for %s has an unknown speed %q", day.Date, model.Speed)
			}
			if *model.Credits == 0 {
				continue
			}
			shares = append(shares, OpenAIAllowanceShare{Date: day.Date, Model: model.Model, Fast: model.Speed == "fast", Percent: *model.Credits})
		}
	}
	return shares, nil
}
