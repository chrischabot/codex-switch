package accounts

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const reserveFilename = "reserve-percent"

func readReservePercent(accountDirectory string) (int, error) {
	data, err := os.ReadFile(filepath.Join(accountDirectory, reserveFilename))
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	percent, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || percent < 0 || percent > 100 {
		return 0, fmt.Errorf("reserve must be a whole percentage from 0 to 100")
	}
	return percent, nil
}

// SetReservePercent keeps this share of every allowance window for direct use.
// Zero disables the reserve; accountName accepts the same names as RemoveAccount.
func SetReservePercent(dataDirectory, accountName string, percent int) (PoolAccount, error) {
	if percent < 0 || percent > 100 {
		return PoolAccount{}, fmt.Errorf("reserve must be a whole percentage from 0 to 100")
	}
	var selected PoolAccount
	err := withPoolLock(dataDirectory, func() error {
		pool, err := ReadPool(dataDirectory)
		if err != nil {
			return err
		}
		for _, account := range pool {
			if account.Name == accountName || account.DisplayName == accountName {
				selected = account
				return writeAtomic(filepath.Join(dataDirectory, account.Name, reserveFilename), percent)
			}
		}
		return fmt.Errorf("unknown account %s", accountName)
	})
	return selected, err
}

// ReserveUnavailableReason applies to the whole account, including windows
// specific to another model. Expired windows no longer hold the account back.
func ReserveUnavailableReason(fact AccountRoutingFact, now time.Time) string {
	if fact.ReservePercent == 0 {
		return ""
	}
	for _, window := range fact.AllowanceSample.Windows {
		if window.ResetsAt.After(now) && window.UsedPercent >= float64(100-fact.ReservePercent) {
			return fmt.Sprintf("held back: %d%% kept for direct use", fact.ReservePercent)
		}
	}
	return ""
}
