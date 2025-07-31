package xlog_test

import (
	"encoding/json"
	goerrors "errors"
	"reflect"
	"testing"
	"time"

	"sync"

	"github.com/effective-security/xlog"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEscapedString(t *testing.T) {
	stru := struct {
		Foo   string
		B     bool
		I     int
		DNull *time.Time
	}{Foo: "foo", B: true, I: -1}

	date, err := time.Parse("2006-01-02", "2021-04-01")
	require.NoError(t, err)

	structVal := struct {
		S      string
		N      int
		D      time.Time
		DPtr   *time.Time
		DNull  *time.Time
		Period time.Duration
	}{
		"str", 1, date, &date, nil, time.Duration(time.Minute * 5),
	}

	errToTest := errors.New("issue: some error")

	assert.Equal(t, `"one (1)"`, xlog.EscapedString(wvs(1)))

	tcases := []struct {
		name string
		val  any
		exp  string
	}{
		{"wvs1", wvs(1), `"one (1)"`},
		{"wvs2", wvs(2), `"two (2)"`},
		{"wvs3", wvs(3), `"more than 2 (3)"`},
		{"ws1", ws(1), `"One"`},
		{"ws2", ws(2), `"Two"`},
		{"ws3", ws(3), `"More than 2"`},
		{"int", 1, "1"},
		{"bytes", []byte(`bytes`), `"Ynl0ZXM="`},
		{"uint", uint(11234123412), "11234123412"},
		{"int64", int64(11234123412), "11234123412"},
		{"uint64", uint64(11234123412), "11234123412"},
		{"nint", -72349568723, "-72349568723"},
		{"bool", false, "false"},
		{"true", true, "true"},
		{"strings", []string{"s1", "s2"}, `["s1","s2"]`},
		{"date", date, `2021-04-01T00:00:00Z`},
		{"date_ptr", &date, `2021-04-01T00:00:00Z`},
		{"duration", 5 * time.Second, `5s`},
		{"struct", structVal, `{"S":"str","N":1,"D":"2021-04-01T00:00:00Z","DPtr":"2021-04-01T00:00:00Z","DNull":null,"Period":300000000000}`},
		{"foo", stru, `{"Foo":"foo","B":true,"I":-1,"DNull":null}`},
		{"foo", reflect.TypeOf(errToTest), `"*errors.fundamental"`},
		{"str", "str", `"str"`},
		{"whitespace", "\t\nstr\n", `"str"`},
		{"err", errToTest.Error(), `"issue: some error"`},
		{"goerrors", goerrors.New("goerrors"), `"goerrors"`},
		{"stringer", xlog.TRACE, `"TRACE"`},
		{"json", json.RawMessage(`{"name":"Faina","age":12,"hobbies":["reading","traveling"]}`), `{"name":"Faina","age":12,"hobbies":["reading","traveling"]}`},
	}

	for _, tc := range tcases {
		assert.Equal(t, tc.exp, xlog.EscapedString(tc.val), tc.name)
	}
}

type ws int32

func (e ws) String() string {
	switch e {
	case 1:
		return "One"
	case 2:
		return "Two"
	}
	return "More than 2"
}

type wvs int32

func (e wvs) ValueString() string {
	switch e {
	case 1:
		return "one"
	case 2:
		return "two"
	}
	return "more than 2"
}

func TestEscapedStringConcurrent(t *testing.T) {
	// Test data that should produce different results
	testData := []any{
		"test string 1",
		"test string 2",
		"test string 3",
		"test string 4",
		"test string 5",
		"test string 6",
		"test string 7",
		"test string 8",
		"test string 9",
		"test string 10",
	}

	// Number of goroutines to run concurrently
	numGoroutines := 100
	// Number of iterations per goroutine
	iterations := 50

	// Channel to collect results
	results := make(chan string, numGoroutines*iterations)

	// Start concurrent goroutines
	var wg sync.WaitGroup
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				// Use different data for each iteration to make corruption more obvious
				dataIndex := (goroutineID + j) % len(testData)
				result := xlog.EscapedString(testData[dataIndex])
				results <- result
			}
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()
	close(results)

	// Collect and verify results
	resultCount := 0
	actualResults := make(map[string]int)

	// Collect all results
	for result := range results {
		resultCount++
		actualResults[result]++
	}

	// Verify we got the expected number of results
	expectedCount := numGoroutines * iterations
	assert.Equal(t, expectedCount, resultCount, "Expected %d results, got %d", expectedCount, resultCount)

	// Calculate expected results and verify each one
	expectedTotal := (numGoroutines * iterations) / len(testData)
	for _, data := range testData {
		expected := xlog.EscapedString(data)
		actualCount := actualResults[expected]
		assert.Equal(t, expectedTotal, actualCount, "Expected result %q should appear %d times, but appeared %d times", expected, expectedTotal, actualCount)
	}
}

func TestEscapedStringConcurrentStress(t *testing.T) {
	// More aggressive test with larger data structures
	type testStruct struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
		Data string `json:"data"`
	}

	// Create test data with different structures
	testData := []any{
		testStruct{ID: 1, Name: "Alice", Data: "data1"},
		testStruct{ID: 2, Name: "Bob", Data: "data2"},
		testStruct{ID: 3, Name: "Charlie", Data: "data3"},
		"simple string 1",
		"simple string 2",
		42,
		true,
		false,
		[]int{1, 2, 3},
		[]string{"a", "b", "c"},
	}

	numGoroutines := 200
	iterations := 100

	results := make(chan string, numGoroutines*iterations)

	var wg sync.WaitGroup
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				dataIndex := (goroutineID + j) % len(testData)
				result := xlog.EscapedString(testData[dataIndex])
				results <- result
			}
		}(i)
	}

	wg.Wait()
	close(results)

	// Collect and verify results
	resultCount := 0
	actualResults := make(map[string]int)

	// Collect all results
	for result := range results {
		resultCount++
		actualResults[result]++
	}

	// Verify we got the expected number of results
	expectedCount := numGoroutines * iterations
	assert.Equal(t, expectedCount, resultCount, "Expected %d results, got %d", expectedCount, resultCount)

	// Calculate expected results and verify each one
	expectedTotal := (numGoroutines * iterations) / len(testData)
	for _, data := range testData {
		expected := xlog.EscapedString(data)
		actualCount := actualResults[expected]
		assert.Equal(t, expectedTotal, actualCount, "Expected result %q should appear %d times, but appeared %d times", expected, expectedTotal, actualCount)
	}
}

func BenchmarkEscapedString(b *testing.B) {
	testData := []any{
		"test string with some content",
		"another test string",
		"third test string",
		42,
		true,
		false,
		[]string{"a", "b", "c"},
		map[string]int{"key1": 1, "key2": 2},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dataIndex := i % len(testData)
		_ = xlog.EscapedString(testData[dataIndex])
	}
}

func BenchmarkEscapedStringConcurrent(b *testing.B) {
	testData := []any{
		"test string with some content",
		"another test string",
		"third test string",
		42,
		true,
		false,
		[]string{"a", "b", "c"},
		map[string]int{"key1": 1, "key2": 2},
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			dataIndex := i % len(testData)
			_ = xlog.EscapedString(testData[dataIndex])
			i++
		}
	})
}
