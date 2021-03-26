package httppipeline

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/megaease/easegateway/pkg/context"
	"github.com/megaease/easegateway/pkg/logger"
	"github.com/megaease/easegateway/pkg/supervisor"
	"github.com/megaease/easegateway/pkg/util/stringtool"

	yaml "gopkg.in/yaml.v2"
)

const (
	// Category is the category of HTTPPipeline.
	Category = supervisor.CategoryPipeline

	// Kind is the kind of HTTPPipeline.
	Kind = "HTTPPipeline"

	// LabelEND is the built-in label for jumping of flow.
	LabelEND = "END"
)

func init() {
	supervisor.Register(&HTTPPipeline{})
}

type (
	// HTTPPipeline is Object HTTPPipeline.
	HTTPPipeline struct {
		super     *supervisor.Supervisor
		superSpec *supervisor.Spec
		spec      *Spec

		runningFilters []*runningFilter
		ht             *context.HTTPTemplate
	}

	runningFilter struct {
		spec       *FilterSpec
		jumpIf     map[string]string
		rootFilter Filter
		filter     Filter
	}

	// Spec describes the HTTPPipeline.
	Spec struct {
		Flow    []Flow                   `yaml:"flow" jsonschema:"omitempty"`
		Filters []map[string]interface{} `yaml:"filters" jsonschema:"-"`
	}

	// Flow controls the flow of pipeline.
	Flow struct {
		Filter string            `yaml:"filter" jsonschema:"required,format=urlname"`
		JumpIf map[string]string `yaml:"jumpIf" jsonschema:"omitempty"`
	}

	// Status contains all status gernerated by runtime, for displaying to users.
	Status struct {
		Health string `yaml:"health"`

		Filters map[string]interface{} `yaml:"filters"`
	}

	// PipelineContext contains the context of the HTTPPipeline.
	PipelineContext struct {
		FilterStats []*FilterStat
	}

	// FilterStat records the statistics of the running filter.
	FilterStat struct {
		Name     string
		Kind     string
		Result   string
		Duration time.Duration
	}
)

func (ps *FilterStat) log() string {
	result := ps.Result
	if result != "" {
		result += ","
	}
	return stringtool.Cat(ps.Name, "(", result, ps.Duration.String(), ")")
}

func (ctx *PipelineContext) log() string {
	if len(ctx.FilterStats) == 0 {
		return "<empty>"
	}

	logs := make([]string, len(ctx.FilterStats))
	for i, filterStat := range ctx.FilterStats {
		logs[i] = filterStat.log()
	}

	return strings.Join(logs, "->")
}

var (
	// context.HTTPContext: *PipelineContext
	runningContexts sync.Map = sync.Map{}
)

func newAndSetPipelineContext(ctx context.HTTPContext) *PipelineContext {
	pipeCtx := &PipelineContext{}

	runningContexts.Store(ctx, pipeCtx)

	return pipeCtx
}

// GetPipelineContext returns the corresponding PipelineContext of the HTTPContext,
// and a bool flag to represent it succeed or not.
func GetPipelineContext(ctx context.HTTPContext) (*PipelineContext, bool) {
	value, ok := runningContexts.Load(ctx)
	if !ok {
		return nil, false
	}

	pipeCtx, ok := value.(*PipelineContext)
	if !ok {
		logger.Errorf("BUG: want *PipelineContext, got %T", value)
		return nil, false
	}

	return pipeCtx, true
}

func deletePipelineContext(ctx context.HTTPContext) {
	runningContexts.Delete(ctx)
}

func marshal(i interface{}) []byte {
	buff, err := yaml.Marshal(i)
	if err != nil {
		panic(fmt.Errorf("marsharl %#v failed: %v", i, err))
	}
	return buff
}

func unmarshal(buff []byte, i interface{}) {
	err := yaml.Unmarshal(buff, i)
	if err != nil {
		panic(fmt.Errorf("unmarshal failed: %v", err))
	}
}

func extractFiltersData(config []byte) interface{} {
	var whole map[string]interface{}
	unmarshal(config, &whole)
	return whole["filters"]
}

func convertToFilterBuffs(obj interface{}) map[string][]byte {
	var filters []map[string]interface{}
	unmarshal(marshal(obj), &filters)

	rst := make(map[string][]byte)
	for _, p := range filters {
		buff := marshal(p)
		meta := &FilterMetaSpec{}
		unmarshal(buff, meta)
		rst[meta.Name] = buff
	}
	return rst
}

func (meta *FilterMetaSpec) validate() error {
	if len(meta.Name) == 0 {
		return fmt.Errorf("filter name is required")
	}
	if len(meta.Kind) == 0 {
		return fmt.Errorf("filter kind is required")
	}

	if meta.Name == LabelEND {
		return fmt.Errorf("can't use %s(built-in label) for filter name", LabelEND)
	}

	return nil
}

// Validate validates Spec.
func (s Spec) Validate(config []byte) (err error) {
	errPrefix := "filters"
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%s: %s", errPrefix, r)
		}
	}()

	filtersData := extractFiltersData(config)
	if filtersData == nil {
		return fmt.Errorf("validate failed: filters is required")
	}
	filterBuffs := convertToFilterBuffs(filtersData)

	filterSpecs := make(map[string]*FilterSpec)
	var templateFilterBuffs []context.FilterBuff
	for _, filterSpec := range s.Filters {
		spec, err := newFilterSpecInternal(filterSpec)
		if err != nil {
			panic(err)
		}

		if _, exists := filterSpecs[spec.Name()]; exists {
			panic(fmt.Errorf("conflict name: %s", spec.Name()))
		}
		filterSpecs[spec.Name()] = spec

		templateFilterBuffs = append(templateFilterBuffs, context.FilterBuff{
			Name: spec.Name(),
			Buff: filterBuffs[spec.Name()],
		})
	}

	// validate http template inside filter specs
	_, err = context.NewHTTPTemplate(templateFilterBuffs)
	if err != nil {
		panic(fmt.Errorf("filter has invalid httptemplate: %v", err))
	}

	errPrefix = "flow"

	filters := make(map[string]struct{})
	for _, f := range s.Flow {
		if _, exists := filters[f.Filter]; exists {
			panic(fmt.Errorf("repeated filter %s", f.Filter))
		}
	}

	labelsValid := map[string]struct{}{LabelEND: {}}
	for i := len(s.Flow) - 1; i >= 0; i-- {
		f := s.Flow[i]
		spec, exists := filterSpecs[f.Filter]
		if !exists {
			panic(fmt.Errorf("filter %s not found", f.Filter))
		}
		expectedResults := spec.RootFilter().Results()
		for result, label := range f.JumpIf {
			if !stringtool.StrInSlice(result, expectedResults) {
				panic(fmt.Errorf("filter %s: result %s is not in %v",
					f.Filter, result, expectedResults))
			}
			if _, exists := labelsValid[label]; !exists {
				panic(fmt.Errorf("filter %s: label %s not found",
					f.Filter, label))
			}
		}
		labelsValid[f.Filter] = struct{}{}
	}

	return nil
}

// Category returns the category of HTTPPipeline.
func (hp *HTTPPipeline) Category() supervisor.ObjectCategory {
	return Category
}

// Kind returns the kind of HTTPPipeline.
func (hp *HTTPPipeline) Kind() string {
	return Kind
}

// DefaultSpec returns the default spec of HTTPPipeline.
func (hp *HTTPPipeline) DefaultSpec() interface{} {
	return &Spec{}
}

// Init initilizes HTTPPipeline.
func (hp *HTTPPipeline) Init(superSpec *supervisor.Spec, super *supervisor.Supervisor) {
	hp.superSpec, hp.spec, hp.super = superSpec, superSpec.ObjectSpec().(*Spec), super
	hp.reload(nil /*no previous generation*/)
}

// Inherit inherits previous generation of HTTPPipeline.
func (hp *HTTPPipeline) Inherit(superSpec *supervisor.Spec,
	previousGeneration supervisor.Object, super *supervisor.Supervisor) {

	hp.superSpec, hp.spec, hp.super = superSpec, superSpec.ObjectSpec().(*Spec), super
	hp.reload(previousGeneration.(*HTTPPipeline))

	// NOTE: It's filters' responsibility to inherit and clean their resources.
	// previousGeneration.Close()
}

func (hp *HTTPPipeline) reload(previousGeneration *HTTPPipeline) {
	runningFilters := make([]*runningFilter, 0)
	if len(hp.spec.Flow) == 0 {
		for _, filterSpec := range hp.spec.Filters {
			spec, err := newFilterSpecInternal(filterSpec)
			if err != nil {
				panic(err)
			}

			runningFilters = append(runningFilters, &runningFilter{
				spec: spec,
			})
		}
	} else {
		for _, f := range hp.spec.Flow {
			var spec *FilterSpec
			for _, filterSpec := range hp.spec.Filters {
				var err error
				spec, err = newFilterSpecInternal(filterSpec)
				if err != nil {
					panic(err)
				}
				if spec.Name() == f.Filter {
					break
				}
			}
			if spec == nil {
				panic(fmt.Errorf("flow filter %s not found in filters", f.Filter))
			}

			runningFilters = append(runningFilters, &runningFilter{
				spec:   spec,
				jumpIf: f.JumpIf,
			})
		}
	}

	var filterBuffs []context.FilterBuff
	for _, runningFilter := range runningFilters {
		name, kind := runningFilter.spec.Name(), runningFilter.spec.Kind()
		rootFilter, exists := filterRegistry[kind]
		if !exists {
			panic(fmt.Errorf("kind %s not found", kind))
		}

		var prevInstance Filter
		if previousGeneration != nil {
			runningFilter := previousGeneration.getRunningFilter(name)
			if runningFilter != nil {
				prevInstance = runningFilter.filter
			}
		}

		filter := reflect.New(reflect.TypeOf(rootFilter).Elem()).Interface().(Filter)
		if prevInstance == nil {
			filter.Init(runningFilter.spec, hp.super)
		} else {
			filter.Inherit(runningFilter.spec, prevInstance, hp.super)
		}

		runningFilter.filter, runningFilter.rootFilter = filter, rootFilter

		filterBuffs = append(filterBuffs, context.FilterBuff{
			Name: name,
			Buff: []byte(runningFilter.spec.YAMLConfig()),
		})
	}

	// creating a valid httptemplates
	var err error
	hp.ht, err = context.NewHTTPTemplate(filterBuffs)
	if err != nil {
		panic(fmt.Errorf("create http template failed %v", err))
	}

	hp.runningFilters = runningFilters
}

func (hp *HTTPPipeline) getNextFilterIndex(index int, result string) int {
	// return index + 1 if last filter succeeded
	if result == "" {
		return index + 1
	}

	// check the jumpIf table of current filter, return its index if the jump
	// target is valid and -1 otherwise
	filter := hp.runningFilters[index]
	if !stringtool.StrInSlice(result, filter.rootFilter.Results()) {
		format := "BUG: invalid result %s not in %v"
		logger.Errorf(format, result, filter.rootFilter.Results())
	}

	if len(filter.jumpIf) == 0 {
		return -1
	}
	name, ok := filter.jumpIf[result]
	if !ok {
		return -1
	}
	if name == LabelEND {
		return len(hp.runningFilters)
	}

	for index++; index < len(hp.runningFilters); index++ {
		if hp.runningFilters[index].spec.Name() == name {
			return index
		}
	}

	return -1
}

func (hp *HTTPPipeline) Handle(ctx context.HTTPContext) {
	pipeCtx := newAndSetPipelineContext(ctx)
	defer deletePipelineContext(ctx)
	ctx.SetTemplate(hp.ht)

	filterIndex := -1

	handle := func(lastResult string) string {
		// Filters are called recursively as a stack and filterIndex is used
		// as a pointer to track the progress, so we need to save the index
		// of previous filter and restore it before return
		lastIndex := filterIndex
		defer func() {
			filterIndex = lastIndex
		}()

		filterIndex = hp.getNextFilterIndex(filterIndex, lastResult)
		if filterIndex == len(hp.runningFilters) {
			return "" // reach the end of pipeline
		} else if filterIndex == -1 {
			return lastResult // an error occurs but no filter can handle it
		}

		filter := hp.runningFilters[filterIndex]
		name := filter.spec.Name()

		if err := ctx.SaveReqToTemplate(name); err != nil {
			format := "save http req failed, dict is %#v err is %v"
			logger.Errorf(format, ctx.Template().GetDict(), err)
		}

		// As filters are called recursively, stats must be added before
		// calling the filter to keep them in a correct order
		stat := &FilterStat{Name: name, Kind: filter.spec.Kind()}
		pipeCtx.FilterStats = append(pipeCtx.FilterStats, stat)

		startTime := time.Now()
		stat.Result = filter.filter.Handle(ctx)
		stat.Duration = time.Since(startTime)

		if err := ctx.SaveRspToTemplate(name); err != nil {
			format := "save http rsp failed, dict is %#v err is %v"
			logger.Errorf(format, ctx.Template().GetDict(), err)
		}

		return stat.Result
	}

	ctx.SetHandlerCaller(handle)
	handle("")

	ctx.AddTag(stringtool.Cat("pipeline: ", pipeCtx.log()))
}

func (hp *HTTPPipeline) getRunningFilter(name string) *runningFilter {
	for _, filter := range hp.runningFilters {
		if filter.spec.Name() == name {
			return filter
		}
	}

	return nil
}

// Status returns Status genreated by Runtime.
func (hp *HTTPPipeline) Status() *supervisor.Status {
	s := &Status{
		Filters: make(map[string]interface{}),
	}

	for _, runningFilter := range hp.runningFilters {
		s.Filters[runningFilter.spec.Name()] = runningFilter.filter.Status()
	}

	return &supervisor.Status{
		ObjectStatus: s,
	}
}

// Close closes HTTPPipeline.
func (hp *HTTPPipeline) Close() {
	for _, runningFilter := range hp.runningFilters {
		runningFilter.filter.Close()
	}
}
