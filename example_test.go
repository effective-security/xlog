package xlog_test

import (
	"os"
	"testing"
	"time"

	"github.com/effective-security/xlog"
	"github.com/pkg/errors"
)

func TestMain(m *testing.M) {
	xlog.TimeNowFn = func() time.Time {
		date, _ := time.Parse("2006-01-02", "2021-04-01")
		return date
	}
	m.Run()
}

func ExampleStringFormatter() {
	var logger = xlog.NewPackageLogger("github.com/effective-security/xlog", "string_formatter").WithValues("prefix", "addon")
	f := xlog.NewStringFormatter(os.Stdout)
	xlog.SetFormatter(f)

	list := []string{"item 1", "item 2"}
	obj := struct {
		Foo     string
		Bar     int
		private string
	}{"foo", 5, "shout not print"}

	f.Format("format", xlog.WARNING, 1, "string1", "string 2", list, obj)
	f.FormatKV("format_kv", xlog.WARNING, 1, "key1", "value 2", "key2", 2, "list", list, "obj", obj)

	logger.KV(xlog.ERROR, "reason", "with time, level, caller", "err", errors.New("just a string").Error(), "number", 123)

	f.Options(xlog.FormatWithLocation)
	logger.KV(xlog.ERROR, "reason", "location",
		"err", errors.New("just a string").Error(),
		"number", 123,
		"list", list,
		"obj", obj,
	)

	f.Options(xlog.FormatSkipTime, xlog.FormatSkipLevel, xlog.FormatNoCaller)
	logger.KV(xlog.ERROR, "reason", "skip time, level, caller",
		"err", errors.New("just a string").Error(),
		"number", 123,
		"list", list,
		"obj", obj,
	)

	// Output:
	// time=2021-04-01T00:00:00Z level=W pkg=format func=ExampleStringFormatter "string1" "string 2" ["item 1","item 2"] {"Foo":"foo","Bar":5}
	// time=2021-04-01T00:00:00Z level=W pkg=format_kv func=ExampleStringFormatter key1="value 2" key2=2 list=["item 1","item 2"] obj={"Foo":"foo","Bar":5}
	// time=2021-04-01T00:00:00Z level=E pkg=string_formatter func=ExampleStringFormatter prefix="addon" reason="with time, level, caller" err="just a string" number=123
	// time=2021-04-01T00:00:00Z level=E pkg=string_formatter src=example_test.go:38 func=ExampleStringFormatter prefix="addon" reason="location" err="just a string" number=123 list=["item 1","item 2"] obj={"Foo":"foo","Bar":5}
	// pkg=string_formatter src=example_test.go:46 prefix="addon" reason="skip time, level, caller" err="just a string" number=123 list=["item 1","item 2"] obj={"Foo":"foo","Bar":5}
}

func ExamplePrettyFormatter() {
	xlog.TimeNowFn = func() time.Time {
		date, _ := time.Parse("2006-01-02", "2021-04-01")
		return date
	}

	var logger = xlog.NewPackageLogger("github.com/effective-security/xlog", "pretty_formatter")
	f := xlog.NewPrettyFormatter(os.Stdout)
	xlog.SetFormatter(f)

	list := []string{"item 1", "item 2"}
	obj := struct {
		Foo     string
		Bar     int
		private string
	}{"foo", 5, "shout not print"}

	f.Format("format", xlog.WARNING, 1, "string1", "string 2", list, obj)
	f.FormatKV("format_kv", xlog.WARNING, 1, "key1", "value 2", "key2", 2, "list", list, "obj", obj)

	logger.KV(xlog.ERROR, "reason", "with time, level, caller", "err", errors.New("just a string").Error(), "number", 123)

	f.Options(xlog.FormatWithLocation)
	logger.KV(xlog.ERROR, "reason", "location",
		"err", errors.New("just a string").Error(),
		"number", 123,
		"list", list,
		"obj", obj,
	)

	f.Options(xlog.FormatSkipTime, xlog.FormatSkipLevel, xlog.FormatNoCaller)
	logger.KV(xlog.ERROR, "reason", "skip time, level, caller",
		"err", errors.New("just a string").Error(),
		"number", 123,
		"list", list,
		"obj", obj,
	)

	// Output:
	// 2021-04-01 00:00:00.000000 W | format: func=ExamplePrettyFormatter, "string1", "string 2", ["item 1","item 2"], {"Foo":"foo","Bar":5}
	// 2021-04-01 00:00:00.000000 W | format_kv: func=ExamplePrettyFormatter, key1="value 2", key2=2, list=["item 1","item 2"], obj={"Foo":"foo","Bar":5}
	// 2021-04-01 00:00:00.000000 E | pretty_formatter: func=ExamplePrettyFormatter, reason="with time, level, caller", err="just a string", number=123
	// 2021-04-01 00:00:00.000000 E | pretty_formatter: src=example_test.go:84, func=ExamplePrettyFormatter, reason="location", err="just a string", number=123, list=["item 1","item 2"], obj={"Foo":"foo","Bar":5}
	// pretty_formatter: src=example_test.go:92, reason="skip time, level, caller", err="just a string", number=123, list=["item 1","item 2"], obj={"Foo":"foo","Bar":5}
}

func ExampleJSONFormatter() {
	xlog.TimeNowFn = func() time.Time {
		date, _ := time.Parse("2006-01-02", "2021-04-01")
		return date
	}

	var logger = xlog.NewPackageLogger("github.com/effective-security/xlog", "json_formatter")
	f := xlog.NewJSONFormatter(os.Stdout)
	xlog.SetFormatter(f)

	list := []string{"item 1", "item 2"}
	obj := struct {
		Foo     string
		Bar     int
		private string
	}{"foo", 5, "shoud not print"}

	f.Format("format", xlog.WARNING, 1, "string1", "string 2", list, obj)
	f.FormatKV("format_kv", xlog.WARNING, 1, "key1", "value 2", "key2", 2, "list", list, "obj", obj)

	logger.KV(xlog.ERROR, "reason", "with time, level, caller", "err", errors.New("just a string").Error(), "number", 123)

	f.Options(xlog.FormatWithLocation)
	logger.KV(xlog.ERROR, "reason", "location",
		"err", errors.New("just a string").Error(),
		"number", 123,
		"list", list,
		"obj", obj,
	)

	f.Options(xlog.FormatSkipTime, xlog.FormatSkipLevel, xlog.FormatNoCaller)
	logger.KV(xlog.ERROR, "reason", "skip time, level, caller",
		"err", errors.New("just a string").Error(),
		"number", 123,
		"list", list,
		"obj", obj,
	)

	// Output:
	// {"func":"ExampleJSONFormatter","level":"W","msg":"string1string 2[item 1 item 2] {foo 5 shoud not print}","pkg":"format","time":"2021-04-01T00:00:00Z"}
	// {"func":"ExampleJSONFormatter","key1":"value 2","key2":2,"level":"W","list":["item 1","item 2"],"obj":{"Foo":"foo","Bar":5},"pkg":"format_kv","time":"2021-04-01T00:00:00Z"}
	// {"err":"just a string","func":"ExampleJSONFormatter","level":"E","number":123,"pkg":"json_formatter","reason":"with time, level, caller","time":"2021-04-01T00:00:00Z"}
	// {"err":"just a string","func":"ExampleJSONFormatter","level":"E","list":["item 1","item 2"],"number":123,"obj":{"Foo":"foo","Bar":5},"pkg":"json_formatter","reason":"location","src":"example_test.go:130","time":"2021-04-01T00:00:00Z"}
	// {"err":"just a string","list":["item 1","item 2"],"number":123,"obj":{"Foo":"foo","Bar":5},"pkg":"json_formatter","reason":"skip time, level, caller","src":"example_test.go:138"}
}
