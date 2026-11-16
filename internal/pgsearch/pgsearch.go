// Package pgsearch is hybrid retrieval over a Postgres table that has a
// pgvector embedding and a tsvector of the same text: the nearest rows by
// cosine and the best keyword matches, fused by reciprocal rank in one
// statement. Knowledge chunks and memories are both searched this way.
package pgsearch

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"unicode"

	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
)

// Args collects a statement's positional arguments. The statement is run on
// the raw connection with $n, because GORM's Raw mishandles the @@ operator.
type Args struct{ vals []any }

// Add appends a value and answers its placeholder. A placeholder may be used
// more than once in the statement.
func (a *Args) Add(v any) string {
	a.vals = append(a.vals, v)
	return fmt.Sprintf("$%d", len(a.vals))
}

// Source is what is searched.
type Source struct {
	// From is the FROM clause, joins included ("memories m").
	From string
	// ID, Embedding and TSV are the row's id, vector and tsvector expressions.
	ID, Embedding, TSV string
	// Where narrows the rows (built with the same Args); "" is every row.
	Where string
	// Candidates is how many rows each half hands to the fusion.
	Candidates int
	// MaxDistance drops a vector-only neighbour farther than this; 0 keeps all.
	MaxDistance float64
}

// Fused is the WITH clause of a hybrid search for vec and query: it ends in a
// CTE `fused(id, score)`, best first by score, which the caller selects from
// and joins back to its rows.
func Fused(a *Args, src Source, vec pgvector.Vector, query string) string {
	where := src.Where
	if where == "" {
		where = "TRUE"
	}
	v, kw := a.Add(vec), a.Add(Keywords(query))
	far := ""
	if src.MaxDistance > 0 {
		far = " WHERE d < " + a.Add(src.MaxDistance)
	}
	n := fmt.Sprint(src.Candidates)
	return `
WITH q AS (
  SELECT plainto_tsquery('simple', ` + kw + `) AS a,
         replace(plainto_tsquery('simple', ` + kw + `)::text, '&', '|')::tsquery AS o
),
vec AS (
  SELECT id, ROW_NUMBER() OVER (ORDER BY d) AS r FROM (
    SELECT ` + src.ID + ` AS id, ` + src.Embedding + ` <=> ` + v + ` AS d
    FROM ` + src.From + `
    WHERE ` + where + `
    ORDER BY ` + src.Embedding + ` <=> ` + v + ` LIMIT ` + n + `
  ) v` + far + `
),
kw AS (
  SELECT id, ROW_NUMBER() OVER (ORDER BY exact DESC, rank DESC) AS r FROM (
    SELECT ` + src.ID + ` AS id, (` + src.TSV + ` @@ q.a) AS exact, ts_rank(` + src.TSV + `, q.o) AS rank
    FROM ` + src.From + ` CROSS JOIN q
    WHERE ` + where + ` AND ` + src.TSV + ` @@ q.o
    ORDER BY (` + src.TSV + ` @@ q.a) DESC, ts_rank(` + src.TSV + `, q.o) DESC LIMIT ` + n + `
  ) k
),
fused AS (
  SELECT id, SUM(1.0 / (60 + r)) AS score FROM (
    SELECT id, r FROM vec UNION ALL SELECT id, r FROM kw
  ) u GROUP BY id
)`
}

// Run executes a statement built with a and hands each row to scan.
func Run(ctx context.Context, gdb *gorm.DB, stmt string, a *Args, scan func(*sql.Rows) error) error {
	return gdb.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		IterativeScan(tx)
		rows, err := tx.Statement.ConnPool.QueryContext(ctx, stmt, a.vals...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if err := scan(rows); err != nil {
				return err
			}
		}
		return rows.Err()
	})
}

// IterativeScan lets a filtered HNSW scan in tx keep going until it has the
// rows asked for: the index covers every Bot, so without it (pgvector 0.8) a
// per-Bot filter can leave fewer than k rows, or none. Older servers lack the
// setting, and a failed SET must not poison the transaction.
func IterativeScan(tx *gorm.DB) {
	tx.Exec("SAVEPOINT iter")
	if err := tx.Exec("SET LOCAL hnsw.iterative_scan = relaxed_order").Error; err != nil {
		tx.Exec("ROLLBACK TO SAVEPOINT iter")
	}
}

// stopwords are dropped from the keyword half so "when do we deploy" does not
// match every chunk that says "we". The vector half still sees the question.
var stopwords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`a an the and or but of to in on at by for from with as is are was were be been
		do does did how what when where who whom why which that this these those it its we you i me my our your
		they them their he she his her there here can could should would will shall may might not no if then than
		about into over under again also any some tell give find show`) {
		stopwords[w] = true
	}
}

// Keywords keeps the query's own words minus stopwords, as typed, so
// Postgres parses "XK-4471-B" the same way it parsed the indexed text.
func Keywords(q string) string {
	var keep []string
	for _, f := range strings.Fields(q) {
		bare := strings.ToLower(strings.TrimFunc(f, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }))
		if bare == "" || stopwords[bare] {
			continue
		}
		keep = append(keep, f)
	}
	if len(keep) > 16 {
		keep = keep[:16]
	}
	return strings.Join(keep, " ")
}
