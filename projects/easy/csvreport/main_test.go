package main

import (
	"math"
	"strings"
	"testing"
)

const goodCSV = `date,region,product,amount
2026-01-05,EU,widget,120.50
2026-01-12,US,widget,99.99
2026-02-02,EU,gadget,250.00
`

func TestParse(t *testing.T) {
	sales, err := parse(strings.NewReader(goodCSV))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(sales) != 3 {
		t.Fatalf("got %d sales, want 3", len(sales))
	}
	first := sales[0]
	if first.Region != "EU" || first.Product != "widget" || first.Amount != 120.50 {
		t.Errorf("first sale = %+v", first)
	}
	if first.Date.Format("2006-01-02") != "2026-01-05" {
		t.Errorf("date = %v", first.Date)
	}
}

func TestParseErrorsCarryRowNumbers(t *testing.T) {
	tests := []struct {
		name    string
		csv     string
		wantErr string
	}{
		{
			"bad date",
			"date,region,product,amount\n2026-01-05,EU,w,1\nnot-a-date,EU,w,2\n",
			"row 3",
		},
		{
			"bad amount",
			"date,region,product,amount\n2026-01-05,EU,w,abc\n",
			"row 2",
		},
		{
			"wrong column count",
			"date,region,product,amount\n2026-01-05,EU,w\n",
			"row 2",
		},
		{
			"wrong header",
			"when,where,what,much\n",
			"unexpected header",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parse(strings.NewReader(tc.csv))
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not mention %q", err, tc.wantErr)
			}
		})
	}
}

func TestAggregate(t *testing.T) {
	sales, err := parse(strings.NewReader(goodCSV))
	if err != nil {
		t.Fatal(err)
	}

	byRegion, byMonth := aggregate(sales)
	if !close2(byRegion["EU"], 370.50) || !close2(byRegion["US"], 99.99) {
		t.Errorf("byRegion = %v", byRegion)
	}
	if !close2(byMonth["2026-01"], 220.49) || !close2(byMonth["2026-02"], 250.00) {
		t.Errorf("byMonth = %v", byMonth)
	}
}

// close2 compares floats to a cent — never == for computed floats.
func close2(a, b float64) bool { return math.Abs(a-b) < 0.005 }
