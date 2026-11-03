//go:build linux

package artifactscope

import "sync"

// Budget accounts for outputs of multiple scopes belonging to one admitted job.
// Never copy it. Charges survive Seal/Release while files remain; successful
// scope deletion refunds its bytes. Native scratch and filesystem blocks need
// separate bounds. This is an in-process write budget, not a durable reservation.
type Budget struct {
	mu          sync.Mutex
	limit, used int64
}

func NewBudget(limit int64) (*Budget, error) {
	if limit < 1 || limit > 1<<40 {
		return nil, ErrLimit
	}
	return &Budget{limit: limit}, nil
}
func (b *Budget) Remaining() int64 { b.mu.Lock(); defer b.mu.Unlock(); return b.limit - b.used }
func (b *Budget) Used() int64      { b.mu.Lock(); defer b.mu.Unlock(); return b.used }
func (b *Budget) reserve(n int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if n < 0 || n > b.limit-b.used {
		return false
	}
	b.used += n
	return true
}
func (b *Budget) refund(n int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if n < 0 || n > b.used {
		panic("artifact budget accounting invariant")
	}
	b.used -= n
}

// NewWithBudget uses the same scope limits/lifecycle as New and also charges a
// shared job output budget. All related stage scopes must receive the same Budget.
func NewWithBudget(root string, inputs map[string]Input, limits Limits, budget *Budget) (*Scope, error) {
	if budget == nil {
		return nil, ErrLimit
	}
	budget.mu.Lock()
	valid := budget.limit > 0
	budget.mu.Unlock()
	if !valid {
		return nil, ErrLimit
	}
	return newScope(root, inputs, limits, budget)
}
