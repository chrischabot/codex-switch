package store

import (
	"fmt"
	"strconv"
	"strings"
)

// AccountIdentity keeps historical usage attached to its account after removal from the pool.
type AccountIdentity struct {
	AccountName       AccountName
	ProviderAccountID string
	Email             string
}

func (database *StoreDatabase) ReadAccountIdentities() ([]AccountIdentity, error) {
	rows, err := database.sqlDatabase.Query(`SELECT account_name, provider_account_id, email FROM account_identities ORDER BY account_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var identities []AccountIdentity
	for rows.Next() {
		var identity AccountIdentity
		if err := rows.Scan(&identity.AccountName, &identity.ProviderAccountID, &identity.Email); err != nil {
			return nil, err
		}
		identities = append(identities, identity)
	}
	return identities, rows.Err()
}

func (database *StoreDatabase) RememberAccountIdentity(identity AccountIdentity) error {
	result, err := database.sqlDatabase.Exec(`
INSERT INTO account_identities (account_name, provider_account_id, email)
VALUES (?, ?, ?)
ON CONFLICT(account_name) DO UPDATE SET email = excluded.email
WHERE account_identities.provider_account_id = excluded.provider_account_id`,
		identity.AccountName, identity.ProviderAccountID, identity.Email)
	if err != nil {
		return fmt.Errorf("remember account identity: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return fmt.Errorf("account name %s already belongs to another ChatGPT account", identity.AccountName)
	}
	return nil
}

// NextAccountNumber is called under the pool mutation lock, after remembering existing accounts.
func (database *StoreDatabase) NextAccountNumber() (int, error) {
	rows, err := database.sqlDatabase.Query(`
SELECT account_name FROM account_identities
UNION SELECT account_name FROM completed_responses
UNION SELECT account_name FROM task_account_pins`)
	if err != nil {
		return 0, fmt.Errorf("read allocated account names: %w", err)
	}
	defer rows.Close()
	next := 1
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return 0, err
		}
		number, err := strconv.Atoi(strings.TrimPrefix(name, "account-"))
		if err != nil {
			return 0, fmt.Errorf("invalid stored account name %q", name)
		}
		if number >= next {
			next = number + 1
		}
	}
	return next, rows.Err()
}
