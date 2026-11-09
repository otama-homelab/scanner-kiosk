package documents

import (
	"database/sql"
	"errors"
)

// InjectContractCommitFailure is test-only wiring for the external backend
// contract fixture; it never enters the production Store API.
func InjectContractCommitFailure(s *Store, committed bool, at ...int) func() {
	previous := s.commit
	count := 0
	s.commit = func(tx *sql.Tx) error {
		count++
		if len(at) > 0 && count != at[0] {
			return previous(tx)
		}
		if committed {
			if err := tx.Commit(); err != nil {
				return err
			}
		}
		return errors.New("injected commit acknowledgement loss")
	}
	return func() { s.commit = previous }
}
