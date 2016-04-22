package handler

import (
	"git.higgsboson.tk/Mic92/kapacitor-scale/scaling"
	"errors"
	"fmt"
	"github.com/influxdata/kapacitor/udf"
	"github.com/influxdata/kapacitor/udf/agent"
	"github.com/pk-rawat/gostr/src"
	"log"
	"strconv"
	"time"
)

type Handler struct {
	Id, When, By    string
	MinInstances    int64
	MaxInstances    int64
	Cooldown        time.Duration
	Simulate, Debug bool
	kapacitorAgent  *agent.Agent
	scaleAgent      *scaling.Agent
}

func New(kapacitorAgent *agent.Agent, scaleAgent *scaling.Agent) *Handler {
	h := Handler{}
	h.kapacitorAgent = kapacitorAgent
	h.scaleAgent = scaleAgent
	return &h
}

// Return the InfoResponse. Describing the properties of this UDF agent.
func (*Handler) Info() (*udf.InfoResponse, error) {
	info := &udf.InfoResponse{
		Wants:    udf.EdgeType_STREAM,
		Provides: udf.EdgeType_STREAM,
		Options: map[string]*udf.OptionInfo{
			"id":            {ValueTypes: []udf.ValueType{udf.ValueType_STRING}},
			"when":          {ValueTypes: []udf.ValueType{udf.ValueType_STRING}},
			"by":            {ValueTypes: []udf.ValueType{udf.ValueType_STRING}},
			"min_instances": {ValueTypes: []udf.ValueType{udf.ValueType_INT}},
			"max_instances": {ValueTypes: []udf.ValueType{udf.ValueType_INT}},
			"cooldown":      {ValueTypes: []udf.ValueType{udf.ValueType_STRING}},
			"simulate":      {ValueTypes: []udf.ValueType{udf.ValueType_BOOL}},
			"debug":         {ValueTypes: []udf.ValueType{udf.ValueType_BOOL}},
		},
	}
	return info, nil
}

func (h *Handler) debug(format string, args ...interface{}) {
	if h.Debug {
		log.Printf(format, args...)
	}
}

// Initialze the handler based of the provided options.
func (h *Handler) Init(r *udf.InitRequest) (*udf.InitResponse, error) {
	init := &udf.InitResponse{Success: true, Error: ""}
	h.Debug = false
	h.Simulate = false
	h.Cooldown = time.Minute
	h.By = "current + 1"
	h.MinInstances = 1
	h.MaxInstances = 3

	var cooldown string
	for _, opt := range r.Options {
		switch opt.Name {
		case "when":
			h.When = opt.Values[0].Value.(*udf.OptionValue_StringValue).StringValue
		case "by":
			h.By = opt.Values[0].Value.(*udf.OptionValue_StringValue).StringValue
		case "min_instances":
			h.MinInstances = opt.Values[0].Value.(*udf.OptionValue_IntValue).IntValue
		case "max_instances":
			h.MaxInstances = opt.Values[0].Value.(*udf.OptionValue_IntValue).IntValue
		case "cooldown":
			cooldown = opt.Values[0].Value.(*udf.OptionValue_StringValue).StringValue
		case "id":
			h.Id = opt.Values[0].Value.(*udf.OptionValue_StringValue).StringValue
		case "simulate":
			h.Simulate = opt.Values[0].Value.(*udf.OptionValue_BoolValue).BoolValue
		case "debug":
			h.Debug = opt.Values[0].Value.(*udf.OptionValue_BoolValue).BoolValue
		}
	}

	if h.When == "" {
		init.Success = false
		init.Error += " must supply `when` expression;"
	}
	if h.By == "" {
		init.Success = false
		init.Error += " must supply `by` expression;"
	}
	if h.MinInstances < 0 {
		init.Success = false
		init.Error += " `MinInstances` must be greater equal 0;"
	}
	if h.MaxInstances < 0 {
		init.Success = false
		init.Error += " `MaxInstances` must be greater equal 0;"
	}

	if h.MaxInstances < h.MinInstances {
		init.Success = false
		init.Error += " `MaxInstances` must be greater equal minimum instances;"
	}

	var err error
	h.Cooldown, err = time.ParseDuration(cooldown)
	if err != nil {
		init.Success = false
		init.Error += fmt.Sprintf(" `cooldown` '%s' has an invalid format: %v", cooldown, err)
	}
	if h.Cooldown < 0 {
		init.Success = false
		init.Error += " `cooldown` must be greater equal 0s"
	}

	return init, nil
}

// Create a snapshot of the running state of the process.
func (o *Handler) Snaphost() (*udf.SnapshotResponse, error) {
	return &udf.SnapshotResponse{}, nil
}

// Restore a previous snapshot.
func (o *Handler) Restore(req *udf.RestoreRequest) (*udf.RestoreResponse, error) {
	return &udf.RestoreResponse{
		Success: true,
	}, nil
}

// Start working with the next batch
func (h *Handler) BeginBatch(begin *udf.BeginBatch) error {
	return errors.New("batching not supported")
}

func (h *Handler) evaluateWhen(p *udf.Point) (bool, error) {
	fields := make(map[string]interface{})
	for k, v := range p.GetFieldsInt() {
		fields[k] = v
	}
	for k, v := range p.GetFieldsDouble() {
		fields[k] = v
	}
	res := gostr.Evaluate(h.When, fields).(string)
	doScale, err := strconv.ParseBool(res)
	if err != nil {
		return false, fmt.Errorf("the expression `when` should evaluate to true or false, got %s", res)
	}
	h.debug("evaluate '%s' for '%v' -> should scale: %v", h.When, fields, doScale)
	return doScale, nil
}

func (h *Handler) evaluateBy(s *scaling.Service) (int64, error) {
	scaleContext := make(map[string]interface{})
	scaleContext["current"] = s.CurrentInstances
	res := gostr.Evaluate(h.By, scaleContext).(string)
	amount, err := strconv.ParseInt(res, 10, 64)
	if err != nil {
		return -1, fmt.Errorf("the expression `scale` should evaluate to an integer value, got %s (tipp: there is an ROUND() method)", res)
	}
	if amount < h.MinInstances {
		return h.MinInstances, nil
	}
	if amount > h.MaxInstances {
		return h.MaxInstances, nil
	}
	return amount, nil
}

func (h *Handler) Point(p *udf.Point) error {
	doScale, err := h.evaluateWhen(p)
	if !doScale {
		return err
	}
	service, err := h.scaleAgent.RequestScaling(h.Id, time.Unix(0, p.Time))
	if err != nil {
		return fmt.Errorf("Failed start scaling: %v", err)
	}
	if service == nil {
		h.debug("skip scaling because of cooldown")
		return nil
	}
	defer service.Unlock()
	to, err := h.evaluateBy(service)
	if err != nil {
		return err
	}
	if to == service.CurrentInstances {
		h.debug("skip scaling service '%s' still %d", h.Id, to)
	}
	h.debug("attempt to scale service '%s' from %d to %d", h.Id, service.CurrentInstances, to)
	if !h.Simulate {
		err = h.scaleAgent.Scale(h.Id, to)
		if err != nil {
			return err
		}
	}
	service.CurrentInstances = to
	service.CooldownUntil = time.Now().Add(h.Cooldown)
	p.FieldsDouble = make(map[string]float64)
	p.FieldsInt = map[string]int64{"scale": to}
	p.FieldsString = make(map[string]string)
	h.kapacitorAgent.Responses <- &udf.Response{
		Message: &udf.Response_Point{
			Point: p,
		},
	}
	return nil
}

func (o *Handler) EndBatch(end *udf.EndBatch) error {
	return nil
}

// Stop the handler gracefully.
func (o *Handler) Stop() {
	close(o.kapacitorAgent.Responses)
}
