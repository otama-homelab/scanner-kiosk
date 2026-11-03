package documents

import (
	"database/sql"
	"errors"
)

// InjectContractCommitFailure is test-only wiring for the external backend
// contract fixture; it never enters the production Store API.
func InjectContractCommitFailure(s *Store, committed bool) func() {
	previous := s.commit
	s.commit = func(tx *sql.Tx) error {
		if committed {
			if err := tx.Commit(); err != nil {
				return err
			}
		}
		return errors.New("injected commit acknowledgement loss")
	}
	return func() { s.commit = previous }
}
