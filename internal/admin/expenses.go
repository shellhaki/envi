package admin

import (
	"context"
	"errors"
	"strings"
	"time"
)

// What the instance costs to run.

// Recurrence says whether an expense repeats: once, monthly or yearly.
const (
	Once    = "once"
	Monthly = "monthly"
	Yearly  = "yearly"
)

// Expense is one thing paid for.
type Expense struct {
	ID          string    `json:"id"`
	AmountMinor int64     `json:"amount_minor"`
	Currency    string    `json:"currency"`
	Category    string    `json:"category"`
	Description string    `json:"description"`
	SpentOn     time.Time `json:"spent_on"`
	Recurring   string    `json:"recurring"`
	CreatedAt   time.Time `json:"created_at"`
}

// CurrencyTotal is the sum for one currency.
type CurrencyTotal struct {
	Currency string `json:"currency"`
	// Everything ever recorded in this currency.
	TotalMinor int64 `json:"total_minor"`
	// What the recurring items cost every month: monthly items at face value, yearly ones divided by twelve.
	MonthlyMinor int64 `json:"monthly_minor"`
	Count        int64 `json:"count"`
}

// CategoryTotal answers "what is the money going on".
type CategoryTotal struct {
	Category   string `json:"category"`
	Currency   string `json:"currency"`
	TotalMinor int64  `json:"total_minor"`
	Count      int64  `json:"count"`
}

// Spending is the expense half of the operator screen.
type Spending struct {
	Totals     []CurrencyTotal `json:"totals"`
	Categories []CategoryTotal `json:"categories"`
	Recent     []Expense       `json:"recent"`
}

var (
	errAmount    = errors.New("amount must be greater than zero")
	errCurrency  = errors.New("currency must be NGN or USD")
	errCategory  = errors.New("category is required")
	errRecurring = errors.New("recurring must be once, monthly or yearly")
)

// AddExpense records one.
func (s Service) AddExpense(ctx context.Context, operatorID string, e Expense) (Expense, error) {
	if e.AmountMinor <= 0 {
		return Expense{}, errAmount
	}
	e.Currency = strings.ToUpper(strings.TrimSpace(e.Currency))
	if e.Currency != "NGN" && e.Currency != "USD" {
		return Expense{}, errCurrency
	}
	e.Category = strings.TrimSpace(e.Category)
	if e.Category == "" {
		return Expense{}, errCategory
	}
	if e.Recurring == "" {
		e.Recurring = Once
	}
	switch e.Recurring {
	case Once, Monthly, Yearly:
	default:
		return Expense{}, errRecurring
	}
	if e.SpentOn.IsZero() {
		e.SpentOn = time.Now().UTC()
	}

	err := s.DB.QueryRow(ctx, `
		INSERT INTO expenses (amount_minor, currency, category, description, spent_on, recurring, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at
	`, e.AmountMinor, e.Currency, e.Category, strings.TrimSpace(e.Description),
		e.SpentOn, e.Recurring, operatorID,
	).Scan(&e.ID, &e.CreatedAt)
	return e, err
}

func (s Service) DeleteExpense(ctx context.Context, id string) error {
	_, err := s.DB.Exec(ctx, `DELETE FROM expenses WHERE id = $1`, id)
	return err
}

// Spending gathers the totals and the recent rows.
func (s Service) Spending(ctx context.Context, limit int) (Spending, error) {
	var out Spending
	var exists bool
	if err := s.DB.QueryRow(ctx, `SELECT to_regclass('public.expenses') IS NOT NULL`).Scan(&exists); err != nil {
		return out, err
	}
	if !exists {
		return Spending{Totals: []CurrencyTotal{}, Categories: []CategoryTotal{}, Recent: []Expense{}}, nil
	}

	totals, err := s.currencyTotals(ctx)
	if err != nil {
		return out, err
	}
	categories, err := s.categoryTotals(ctx)
	if err != nil {
		return out, err
	}
	recent, err := s.recentExpenses(ctx, limit)
	if err != nil {
		return out, err
	}
	return Spending{Totals: totals, Categories: categories, Recent: recent}, nil
}

func (s Service) currencyTotals(ctx context.Context) ([]CurrencyTotal, error) {
	// The monthly figure divides yearly costs by twelve in integer arithmetic.
	rows, err := s.DB.Query(ctx, `
		SELECT currency,
		       sum(amount_minor),
		       sum(CASE recurring
		             WHEN 'monthly' THEN amount_minor
		             WHEN 'yearly'  THEN amount_minor / 12
		             ELSE 0
		           END),
		       count(*)
		  FROM expenses
		 GROUP BY currency
		 ORDER BY currency
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []CurrencyTotal{}
	for rows.Next() {
		var t CurrencyTotal
		if err := rows.Scan(&t.Currency, &t.TotalMinor, &t.MonthlyMinor, &t.Count); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s Service) categoryTotals(ctx context.Context) ([]CategoryTotal, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT category, currency, sum(amount_minor), count(*)
		  FROM expenses
		 GROUP BY category, currency
		 ORDER BY sum(amount_minor) DESC
		 LIMIT 12
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []CategoryTotal{}
	for rows.Next() {
		var c CategoryTotal
		if err := rows.Scan(&c.Category, &c.Currency, &c.TotalMinor, &c.Count); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s Service) recentExpenses(ctx context.Context, limit int) ([]Expense, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT id, amount_minor, currency, category, description, spent_on, recurring, created_at
		  FROM expenses
		 ORDER BY spent_on DESC, created_at DESC
		 LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Expense{}
	for rows.Next() {
		var e Expense
		if err := rows.Scan(&e.ID, &e.AmountMinor, &e.Currency, &e.Category,
			&e.Description, &e.SpentOn, &e.Recurring, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
