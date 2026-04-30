package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// rollbackOnError rolls back tx and joins any unexpected rollback error into *err.
// After a successful Commit, Rollback is a no-op (returns sql.ErrTxDone) and is ignored.
// When commit fails or an earlier step returned an error, a non-ErrTxDone rollback
// failure is joined into *err so operators see data-at-risk signals.
//
// Usage requires a named error return on the enclosing function:
//
//	func (s *Store) Foo() (err error) {
//		tx, err := s.db.Begin()
//		if err != nil { return ... }
//		defer func() { rollbackOnError(tx, &err) }()
//		...
//	}
func rollbackOnError(tx *sql.Tx, err *error) {
	rbErr := tx.Rollback()
	if rbErr == nil || errors.Is(rbErr, sql.ErrTxDone) {
		return
	}
	wrapped := fmt.Errorf("rollback: %w", rbErr)
	if *err == nil {
		*err = wrapped
		return
	}
	*err = errors.Join(*err, wrapped)
}
