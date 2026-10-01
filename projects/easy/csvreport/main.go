// csvreport aggregates a sales CSV (date,region,product,amount) into
// per-region and per-month summaries.
//
//	csvreport testdata/sales.csv
package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"text/tabwriter"
	"time"
)

type Sale struct {
	Date    time.Time
	Region  string
	Product string
	Amount  float64 // fine for a report; a ledger would use integer cents (see gobank)
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: csvreport <file.csv>")
		os.Exit(2)
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "csvreport:", err)
		os.Exit(1)
	}
	defer f.Close()

	sales, err := parse(f)
	if err != nil {
		fmt.Fprintln(os.Stderr, "csvreport:", err)
		os.Exit(1)
	}
	report(os.Stdout, sales)
}

// parse reads all rows. Any bad row fails the whole file — with the row
// number in the error, which is the difference between a five-second fix
// and an hour of binary-searching a 100k-line CSV.
func parse(r io.Reader) ([]Sale, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = 4 // the reader enforces column count for us

	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	if header[0] != "date" {
		return nil, fmt.Errorf("unexpected header %v, want date,region,product,amount", header)
	}

	var sales []Sale
	for row := 2; ; row++ {
		rec, err := cr.Read()
		if err == io.EOF {
			return sales, nil
		}
		if err != nil {
			return nil, fmt.Errorf("row %d: %w", row, err)
		}

		// Reference date layout: 2006-01-02 IS the format string.
		date, err := time.Parse("2006-01-02", rec[0])
		if err != nil {
			return nil, fmt.Errorf("row %d: bad date: %w", row, err)
		}
		amount, err := strconv.ParseFloat(rec[3], 64)
		if err != nil {
			return nil, fmt.Errorf("row %d: bad amount: %w", row, err)
		}

		sales = append(sales, Sale{Date: date, Region: rec[1], Product: rec[2], Amount: amount})
	}
}

type totals map[string]float64

func aggregate(sales []Sale) (byRegion, byMonth totals) {
	byRegion = make(totals)
	byMonth = make(totals)
	for _, s := range sales {
		byRegion[s.Region] += s.Amount
		byMonth[s.Date.Format("2006-01")] += s.Amount
	}
	return byRegion, byMonth
}

func report(w io.Writer, sales []Sale) {
	byRegion, byMonth := aggregate(sales)

	var grand float64
	for _, v := range byRegion {
		grand += v
	}
	fmt.Fprintf(w, "%d sales, grand total %.2f\n\n", len(sales), grand)

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	printTotals(tw, "REGION", byRegion, grand)
	fmt.Fprintln(tw, "\t\t")
	printTotals(tw, "MONTH", byMonth, grand)
	tw.Flush()
}

func printTotals(w io.Writer, label string, t totals, grand float64) {
	fmt.Fprintf(w, "%s\tTOTAL\tSHARE\n", label)
	for _, key := range sortedKeys(t) {
		fmt.Fprintf(w, "%s\t%.2f\t%.1f%%\n", key, t[key], t[key]/grand*100)
	}
}

// sortedKeys: map iteration order is deliberately random in Go — any report
// (or test!) needs an explicit order.
func sortedKeys(t totals) []string {
	keys := make([]string, 0, len(t))
	for k := range t {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
