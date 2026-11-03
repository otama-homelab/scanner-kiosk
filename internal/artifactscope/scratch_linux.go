//go:build linux

package artifactscope

import (
	"context"
	"scanner-kiosk/pkg/ocrplugin"
)

type scratchReservation struct {
	scope         *Scope
	budget        *Budget
	input, charge int64
	released      bool
}

func (s *Scope) ReserveScratch(ctx context.Context, inputID string, resultBytes, diagnosticBytes int64) (ocrplugin.ScratchReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.active(ctx); err != nil {
		return nil, err
	}
	input, exists := s.inputs[inputID]
	if !exists {
		return nil, ErrInput
	}
	if resultBytes < 1 || resultBytes > 1<<40 || diagnosticBytes < 0 || diagnosticBytes > 1<<40 {
		return nil, ErrLimit
	}
	if s.budget == nil {
		budget, err := NewBudget(s.limits.MaxTotalOutputBytes)
		if err != nil {
			return nil, err
		}
		// Existing outputs become part of the same budget before scratch can borrow.
		if !budget.reserve(s.used) {
			return nil, ErrLimit
		}
		s.budget = budget
	}
	charge := input.Size + 1 + resultBytes + diagnosticBytes
	if !s.budget.reserve(charge) {
		s.failure = ErrLimit
		return nil, ErrLimit
	}
	lease := &scratchReservation{scope: s, budget: s.budget, input: input.Size, charge: charge}
	if s.scratch == nil {
		s.scratch = make(map[*scratchReservation]bool)
	}
	s.scratch[lease] = true
	return lease, nil
}
func (r *scratchReservation) InputBytes() int64 { return r.input }
func (r *scratchReservation) ReleaseAfterCleanup() {
	r.scope.mu.Lock()
	defer r.scope.mu.Unlock()
	if r.released {
		return
	}
	r.released = true
	r.budget.refund(r.charge)
	delete(r.scope.scratch, r)
}

var _ ocrplugin.ScratchAccess = (*Scope)(nil)
