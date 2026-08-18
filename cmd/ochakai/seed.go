package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// The empty base is the first thing a new deployment has, and the last
// thing anybody wants to look at. Catalogs solve it with connectors that
// crawl the warehouse; ochakai does not have those and will not
// (design doc 0081 §1: a harvester inside the server would need warehouse
// credentials it deliberately does not hold, and knowledge here is
// curated rather than collected).
//
// This is the third way. The warehouse is read by the operator's own
// client, with their own credentials — `bq query`, a Snowflake session,
// psql — and its answer is piped in here. ochakai turns those rows into
// draft concepts and prints them as an OKF bundle, which `ochakai import`
// writes like any other bundle:
//
//	bq query --max_rows=100000 --format=json --nouse_legacy_sql \
//	  'SELECT table_schema, table_name, column_name, data_type, is_nullable
//	     FROM `proj.dataset.INFORMATION_SCHEMA.COLUMNS` ORDER BY ordinal_position' |
//	  ochakai seed - | ochakai import -
//
// The row limit in that first line is not decoration. `bq query` prints
// the first 100 rows and says nothing about the rest, so a dataset with
// more columns than that seeds only the tables those rows reached — with
// no error anywhere, because nothing downstream can tell a truncated
// listing from a small one. seedTruncationNote below is what ochakai can
// say about it from where it stands.
//
// Nothing here connects to anything. No warehouse client is linked into
// the binary, no credential is read, and the same command serves any
// warehouse whose catalog can be spelled as the rows below — which is
// most of them, because INFORMATION_SCHEMA is the one thing they agree
// on.
//
// **What comes out is a draft, and that is the whole point.** A projected
// table entry is a skeleton somebody has to fill in: what the table is
// for, which column lies, when the load is late. Drafts land in the
// review queue where that gets written, and until a human rules on one it
// is marked as what it is (design doc 0069). A catalog's connector
// produces a thousand rows nobody vouches for; this produces a thousand
// questions, which is the honest shape of the same information.

// seedColumn is one row of the schema listing, spelled the way
// INFORMATION_SCHEMA.COLUMNS spells it. Unknown keys are ignored, so a
// `SELECT *` works and so does a hand-written export.
type seedColumn struct {
	Schema     string `json:"table_schema"`
	Table      string `json:"table_name"`
	Column     string `json:"column_name"`
	DataType   string `json:"data_type"`
	IsNullable string `json:"is_nullable"` // "YES" / "NO", as the standard has it
	Comment    string `json:"description"` // BigQuery's column description, when selected
}

// seedTable is the rows of one table, gathered.
type seedTable struct {
	schema, name string
	columns      []seedColumn
}

func cmdSeed(_ context.Context, args []string) error {
	fs := newBareFlagSet(
		"seed",
		"Usage: ochakai seed [flags] <file.json | ->\n\n"+
			"Turn a warehouse's own schema listing into a bundle of draft concepts,\n"+
			"printed as a tar.gz on stdout. One `BigQuery Table` concept per table,\n"+
			"with its columns as a markdown table and its address as `resource`.\n"+
			"Reads JSON rows (an array, or one object per line) with the\n"+
			"INFORMATION_SCHEMA.COLUMNS column names: table_schema, table_name,\n"+
			"column_name, data_type, is_nullable, and description where there is one.\n"+
			"Rows for the same table are gathered however they arrive.\n\n"+
			"ochakai connects to no warehouse and holds no credential of one: you run\n"+
			"the query, with your own client and your own identity, and pipe the answer\n"+
			"here. Every concept comes out as a draft, because a projected schema is a\n"+
			"skeleton somebody still has to say something about — which is what the\n"+
			"review queue is for.\n\n"+
			"Raise your client's row limit when you run that query: `bq query` prints\n"+
			"the first 100 rows unless --max_rows says otherwise, and a listing cut\n"+
			"there looks exactly like a small dataset from here.",
		"  bq query --max_rows=100000 --format=json --nouse_legacy_sql \\\n"+
			"    'SELECT table_schema, table_name, column_name, data_type, is_nullable\n"+
			"       FROM `proj.dataset.INFORMATION_SCHEMA.COLUMNS` ORDER BY ordinal_position' |\n"+
			"    ochakai seed - | ochakai import -\n"+
			"  ochakai seed columns.json > catalog.tar.gz   # look before you import\n"+
			"  ochakai seed --project my-proj --prefix warehouse/tables - | ochakai import -\n")
	project := fs.String("project", "", "the warehouse project or account the tables live in, written into each concept's `resource` address")
	prefix := fs.String("prefix", "tables", "the id prefix each concept is written under, e.g. tables/shop/orders")
	pos, err := exactArgs(fs, args, 1)
	if err != nil {
		return err
	}
	in := os.Stdin
	if pos[0] != "-" {
		f, err := os.Open(pos[0])
		if err != nil {
			return err
		}
		defer f.Close()
		in = f
	}
	cols, err := readSeedColumns(in)
	if err != nil {
		return err
	}
	if note := seedTruncationNote(len(cols)); note != "" {
		fmt.Fprintln(os.Stderr, "note:", note)
	}
	tables := gatherSeedTables(cols)
	if len(tables) == 0 {
		return fmt.Errorf("no rows with a table_name: is this the output of a SELECT over INFORMATION_SCHEMA.COLUMNS?")
	}
	if err := writeSeedBundle(os.Stdout, tables, *project, *prefix); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "seeded %d tables as drafts; pipe into `ochakai import -` to write them\n", len(tables))
	return nil
}

// clientDefaultRows is what `bq query` prints when nobody passes
// --max_rows, and the shape of the one failure this path has that nothing
// else can catch: the pipe succeeds, the bundle is well-formed, and the
// tables past the cut are simply not in it. A listing truncated there is
// indistinguishable from a small warehouse — except for its size, which is
// this exact number and rarely anything else by accident.
const clientDefaultRows = 100

// seedTruncationNote is that suspicion, said out loud once and never as an
// error: exactly 100 rows is a legitimate listing often enough that
// refusing it would be wrong, and a silent partial catalog is the thing
// somebody actually lost a day to.
func seedTruncationNote(rows int) string {
	if rows != clientDefaultRows {
		return ""
	}
	return fmt.Sprintf("the input is exactly %d rows, which is what a client's default row limit looks like:\n"+
		"      `bq query` prints %d unless you pass --max_rows, and drops the rest without saying so.\n"+
		"      If the schema is larger than that, rerun the query with the limit raised.", clientDefaultRows, clientDefaultRows)
}

// readSeedColumns accepts both shapes a warehouse client prints: one JSON
// array (bq --format=json), and one object per line (jq -c, psql's
// row_to_json). Telling them apart by the first non-space byte costs
// nothing and saves a flag nobody would remember.
func readSeedColumns(r io.Reader) ([]seedColumn, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, fmt.Errorf("no input: pass the schema listing on stdin, or name a file")
	}
	if strings.HasPrefix(trimmed, "[") {
		var cols []seedColumn
		if err := json.Unmarshal([]byte(trimmed), &cols); err != nil {
			return nil, fmt.Errorf("reading the JSON array: %w", err)
		}
		return cols, nil
	}
	var cols []seedColumn
	dec := json.NewDecoder(strings.NewReader(trimmed))
	for {
		var c seedColumn
		if err := dec.Decode(&c); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("reading a JSON object per line: %w", err)
		}
		cols = append(cols, c)
	}
	return cols, nil
}

// gatherSeedTables groups columns by table, in the order the rows
// arrived: a listing ordered by ordinal_position keeps its column order,
// which is the order somebody reading the table expects.
func gatherSeedTables(cols []seedColumn) []seedTable {
	index := map[string]int{}
	var tables []seedTable
	for _, c := range cols {
		if c.Table == "" || c.Column == "" {
			continue
		}
		key := c.Schema + "." + c.Table
		i, ok := index[key]
		if !ok {
			i = len(tables)
			index[key] = i
			tables = append(tables, seedTable{schema: c.Schema, name: c.Table})
		}
		tables[i].columns = append(tables[i].columns, c)
	}
	sort.SliceStable(tables, func(i, j int) bool {
		if tables[i].schema != tables[j].schema {
			return tables[i].schema < tables[j].schema
		}
		return tables[i].name < tables[j].name
	})
	return tables
}

// writeSeedBundle prints the tables as an OKF bundle: one markdown
// document per table, at the path its id gives it (design doc 0046 §3.5).
func writeSeedBundle(w io.Writer, tables []seedTable, project, prefix string) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	for _, t := range tables {
		doc := seedDocument(t, project)
		name := path.Join(seedID(t, prefix) + ".md")
		if err := tw.WriteHeader(&tar.Header{
			Name: name, Mode: 0o644, Size: int64(len(doc)),
			ModTime: time.Time{}, Typeflag: tar.TypeReg,
		}); err != nil {
			return err
		}
		if _, err := tw.Write([]byte(doc)); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

// seedID is the concept's address: the prefix, then the schema and table
// as path segments, so a dataset is a directory in the bundle and the
// browse view groups by it for free.
func seedID(t seedTable, prefix string) string {
	segments := []string{}
	for _, s := range []string{prefix, t.schema, t.name} {
		if s = domain.Normalize(strings.Trim(s, "/")); s != "" {
			segments = append(segments, s)
		}
	}
	return path.Join(segments...)
}

// seedDocument renders one table as an OKF document. It says only what
// the schema said — the prose is a prompt for the person who fills it in,
// not a description invented on their behalf. An empty description is
// deliberate: the frontmatter would otherwise carry a sentence nobody
// wrote and everybody would then trust.
func seedDocument(t seedTable, project string) string {
	b := &strings.Builder{}
	fmt.Fprintf(b, "---\ntype: %s\n", domain.TypeTables)
	if resource := seedResource(t, project); resource != "" {
		fmt.Fprintf(b, "resource: %s\n", resource)
	}
	fmt.Fprintf(b, "title: %s\nstatus: draft\n---\n\n", seedTitle(t))
	b.WriteString("Projected from the warehouse's own schema listing. **Nothing below was\n" +
		"written by a person yet** — what this table is for, which column lies, and\n" +
		"when the load is late are the reasons anybody will read this entry, and the\n" +
		"schema does not know any of them.\n\n")
	b.WriteString("| Column | Type | Null | Note |\n|---|---|---|---|\n")
	for _, c := range t.columns {
		null := ""
		if strings.EqualFold(c.IsNullable, "YES") {
			null = "yes"
		}
		fmt.Fprintf(b, "| `%s` | %s | %s | %s |\n",
			c.Column, c.DataType, null, strings.ReplaceAll(c.Comment, "|", "\\|"))
	}
	return b.String()
}

func seedTitle(t seedTable) string {
	if t.schema == "" {
		return t.name
	}
	return t.schema + "." + t.name
}

// seedResource is the table's address in the warehouse, in the spelling
// the demo bundle uses. Without --project the account is unknown and the
// key is left out rather than guessed: a resource that names the wrong
// project is worse than one that names none.
func seedResource(t seedTable, project string) string {
	if project == "" || t.name == "" {
		return ""
	}
	return "bigquery://" + project + "." + seedTitle(t)
}
