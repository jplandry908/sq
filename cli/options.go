package cli

import (
	"fmt"
	"strings"

	"github.com/neilotoole/sq/libsq/core/timez"

	"github.com/neilotoole/sq/drivers"
	"github.com/neilotoole/sq/drivers/csv"
	"github.com/neilotoole/sq/libsq/core/errz"
	"github.com/neilotoole/sq/libsq/core/options"
	"github.com/neilotoole/sq/libsq/driver"
	"github.com/neilotoole/sq/libsq/source"
	"github.com/samber/lo"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/exp/slices"
)

// getOptionsFromFlags builds options.Options from flags. In effect, a flag
// such as --ingest.header is mapped to an option.Opt of the same name.
//
// See also: getOptionsFromCmd, applySourceOptions, applyCollectionOptions.
func getOptionsFromFlags(flags *pflag.FlagSet, reg *options.Registry) (options.Options, error) {
	o := options.Options{}
	err := reg.Visit(func(opt options.Opt) error {
		key := opt.Key()
		f := flags.Lookup(key)
		if f == nil {
			return nil
		}

		if !f.Changed {
			return nil
		}

		if f.Value == nil {
			// This shouldn't happen
			return nil
		}

		o[key] = f.Value.String()
		return nil
	})
	if err != nil {
		return nil, err
	}

	o, err = reg.Process(o)
	if err != nil {
		return nil, errz.Wrap(err, "options from flags")
	}

	return o, nil
}

// getSrcOptionsFromFlags returns the options.Options applicable to
// the driver type.
//
// See also: getOptionsFromFlags, getOptionsFromCmd, applySourceOptions, applyCollectionOptions.
func getSrcOptionsFromFlags(flags *pflag.FlagSet, reg *options.Registry,
	typ source.DriverType,
) (options.Options, error) {
	srcOpts := filterOptionsForSrc(typ, reg.Opts()...)
	srcReg := &options.Registry{}
	srcReg.Add(srcOpts...)
	return getOptionsFromFlags(flags, srcReg)
}

// getOptionsFromCmd returns the options.Options generated by merging
// config options and flag options.
//
// See also: getOptionsFromFlags, applySourceOptions, applyCollectionOptions.
func getOptionsFromCmd(cmd *cobra.Command) (options.Options, error) {
	rc := RunContextFrom(cmd.Context())
	var configOpts options.Options
	if rc.Config != nil && rc.Config.Options != nil {
		configOpts = rc.Config.Options
	} else {
		configOpts = options.Options{}
	}

	flagOpts, err := getOptionsFromFlags(cmd.Flags(), rc.OptionsRegistry)
	if err != nil {
		return nil, err
	}

	return options.Merge(configOpts, flagOpts), nil
}

// applySourceOptions merges options from config, src, and flags.
// The src.Options field may be replaced or mutated. It will always
// be non-nil (unless an error is returned).
//
// See also: getOptionsFromFlags, getOptionsFromCmd, applyCollectionOptions.
func applySourceOptions(cmd *cobra.Command, src *source.Source) error {
	rc := RunContextFrom(cmd.Context())

	defaultOpts := rc.Config.Options
	if defaultOpts == nil {
		defaultOpts = options.Options{}
	}

	flagOpts, err := getOptionsFromFlags(cmd.Flags(), rc.OptionsRegistry)
	if err != nil {
		return err
	}

	srcOpts := src.Options
	if srcOpts == nil {
		srcOpts = options.Options{}
	}

	effectiveOpts := options.Merge(defaultOpts, srcOpts, flagOpts)
	src.Options = effectiveOpts
	return nil
}

// applyCollectionOptions invokes applySourceOptions for
// each source in coll. The sources may have their Source.Options field
// mutated.
//
// See also: getOptionsFromCmd, getOptionsFromFlags, applySourceOptions.
func applyCollectionOptions(cmd *cobra.Command, coll *source.Collection) error {
	return coll.Visit(func(src *source.Source) error {
		return applySourceOptions(cmd, src)
	})
}

// RegisterDefaultOpts registers the options.Opt instances
// that the CLI knows about.
func RegisterDefaultOpts(reg *options.Registry) {
	reg.Add(
		OptFormat,
		OptDatetimeFormat,
		OptDatetimeFormatAsNumber,
		OptDateFormat,
		OptDateFormatAsNumber,
		OptTimeFormat,
		OptTimeFormatAsNumber,
		OptVerbose,
		OptPrintHeader,
		OptMonochrome,
		OptCompact,
		OptPingTimeout,
		OptShellCompletionTimeout,
		OptLogEnabled,
		OptLogFile,
		OptLogLevel,
		driver.OptConnMaxOpen,
		driver.OptConnMaxIdle,
		driver.OptConnMaxIdleTime,
		driver.OptConnMaxLifetime,
		driver.OptMaxRetryInterval,
		driver.OptTuningErrgroupLimit,
		driver.OptTuningRecChanSize,
		OptTuningFlushThreshold,
		drivers.OptIngestHeader,
		drivers.OptIngestSampleSize,
		csv.OptDelim,
		csv.OptEmptyAsNull,
	)
}

// filterOptionsForSrc returns a new slice containing only those
// opts that are applicable to src.
func filterOptionsForSrc(typ source.DriverType, opts ...options.Opt) []options.Opt {
	if len(opts) == 0 {
		return opts
	}

	opts = lo.Reject(opts, func(opt options.Opt, index int) bool {
		if opt == nil {
			return true
		}

		tags := opt.Tags()
		if len(tags) == 0 {
			return true
		}

		// Every source opt has tag "source".
		if !slices.Contains(tags, "source") {
			return true
		}

		key := opt.Key()
		// Let's say the src has driver type "xlsx".
		// If the opt has key "driver.csv.delim", we want to reject it.
		// Thus, if the key has contains "driver", then it must also contain
		// the src driver type.
		if strings.Contains(key, "driver") && !strings.Contains(key, string(typ)) {
			return true
		}

		return false
	})

	return opts
}

// addOptionFlag adds a flag derived from opt to flags, returning the key.
func addOptionFlag(flags *pflag.FlagSet, opt options.Opt) (key string) {
	key = opt.Key()
	switch opt := opt.(type) {
	case options.Int:
		if opt.Short() == 0 {
			flags.Int(key, opt.Get(nil), opt.Usage())
			return key
		}

		flags.IntP(key, string(opt.Short()), opt.Get(nil), opt.Usage())
		return key
	case options.Bool:
		if opt.Short() == 0 {
			flags.Bool(key, opt.Get(nil), opt.Usage())
			return key
		}

		flags.BoolP(key, string(opt.Short()), opt.Get(nil), opt.Usage())
		return
	case options.Duration:
		if opt.Short() == 0 {
			flags.Duration(key, opt.Get(nil), opt.Usage())
			return key
		}

		flags.DurationP(key, string(opt.Short()), opt.Get(nil), opt.Usage())
	default:
		// Treat as string
	}

	defVal := ""
	if v := opt.GetAny(nil); v != nil {
		defVal = fmt.Sprintf("%v", v)
	}

	if opt.Short() == 0 {
		flags.String(key, defVal, opt.Usage())
		return key
	}

	flags.StringP(key, string(opt.Short()), defVal, opt.Usage())
	return key
}

// addTimeFormatOptsFlags adds the time format Opts flags to cmd.
// See: OptDatetimeFormat, OptDateFormat, OptTimeFormat.
func addTimeFormatOptsFlags(cmd *cobra.Command) {
	key := addOptionFlag(cmd.Flags(), OptDatetimeFormat)
	panicOn(cmd.RegisterFlagCompletionFunc(key, completeStrings(-1, timez.NamedLayouts()...)))
	key = addOptionFlag(cmd.Flags(), OptDatetimeFormatAsNumber)
	panicOn(cmd.RegisterFlagCompletionFunc(key, completeBool))

	key = addOptionFlag(cmd.Flags(), OptDateFormat)
	panicOn(cmd.RegisterFlagCompletionFunc(key, completeStrings(-1, timez.NamedLayouts()...)))
	key = addOptionFlag(cmd.Flags(), OptDateFormatAsNumber)
	panicOn(cmd.RegisterFlagCompletionFunc(key, completeBool))

	key = addOptionFlag(cmd.Flags(), OptTimeFormat)
	panicOn(cmd.RegisterFlagCompletionFunc(key, completeStrings(-1, timez.NamedLayouts()...)))
	key = addOptionFlag(cmd.Flags(), OptTimeFormatAsNumber)
	panicOn(cmd.RegisterFlagCompletionFunc(key, completeBool))
}