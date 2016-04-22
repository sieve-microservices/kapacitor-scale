package handler_test

import (
	"git.higgsboson.tk/Mic92/kapacitor-scaling/handler"
	"git.higgsboson.tk/Mic92/kapacitor-scaling/rancher"
	"git.higgsboson.tk/Mic92/kapacitor-scaling/scaling"
	"bufio"
	"flag"
	"fmt"
	"github.com/influxdata/kapacitor/udf"
	"github.com/influxdata/kapacitor/udf/agent"
	"github.com/jarcoal/httpmock"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

func ok(tb testing.TB, err error) {
	if err != nil {
		_, file, line, _ := runtime.Caller(1)
		fmt.Printf("\033[31m%s:%d: unexpected error: %s\033[39m\n\n", filepath.Base(file), line, err.Error())
		tb.FailNow()
	}
}

var cwd_arg = flag.String("cwd", "", "set cwd")

func init() {
	flag.Parse()
	if *cwd_arg != "" {
		if err := os.Chdir(*cwd_arg); err != nil {
			fmt.Println("Chdir error:", err)
		}
	}
}

type server struct {
	Fd          *os.File
	In          *bufio.Reader
	t           *testing.T
	responseBuf []byte
}

func (s *server) writeRequest(req *udf.Request) {
	err := udf.WriteMessage(req, s.Fd)
	ok(s.t, err)
}

func (s *server) writePoint(point *udf.Point) {
	req := &udf.Request{
		Message: &udf.Request_Point{point},
	}
	s.writeRequest(req)
}

func (s *server) ReadResponse() *udf.Response {
	response := new(udf.Response)
	ok(s.t, udf.ReadMessage(&s.responseBuf, s.In, response))
	return response
}

func (s *server) Close() {
	s.Fd.Close()
}

func fakeConnection(t *testing.T) (server, *os.File) {
	fds, err := syscall.Socketpair(syscall.AF_LOCAL, syscall.SOCK_STREAM, 0)
	srvFile := os.NewFile(uintptr(fds[0]), "server")
	clientFile := os.NewFile(uintptr(fds[1]), "client")
	ok(t, err)
	s := server{Fd: srvFile, t: t, In: bufio.NewReader(srvFile)}
	return s, clientFile
}

const RANCHER_URL = "http://secret:accesskey@localhost:8080"

func setupAgent(t *testing.T, connection *os.File) *agent.Agent {
	a := agent.New(connection, connection)
	u, err := url.Parse(RANCHER_URL)
	ok(t, err)

	scaleAgent := *scaling.New(rancher.New(*u))
	h := handler.New(a, &scaleAgent)
	a.Handler = h
	ok(t, a.Start())
	go func() {
		err = a.Wait()
		if err != nil {
			t.Fatal(err)
		}
	}()
	return a
}

func strOpt(name string, value string) *udf.Option {
	return &udf.Option{
		Name: name,
		Values: []*udf.OptionValue{
			&udf.OptionValue{
				udf.ValueType_STRING,
				&udf.OptionValue_StringValue{value},
			},
		},
	}
}

func intOpt(name string, value int64) *udf.Option {
	return &udf.Option{
		Name: name,
		Values: []*udf.OptionValue{
			&udf.OptionValue{
				udf.ValueType_INT,
				&udf.OptionValue_IntValue{value},
			},
		},
	}
}

var options = []*udf.Option{
	strOpt("id", "abc"),
	strOpt("when", "cpu_usage > 8"),
	strOpt("by", "current + 2"),
	intOpt("min_instances", 1),
	intOpt("max_instances", 10),
	strOpt("cooldown", "1m"),
	&udf.Option{
		Name: "simulate",
		Values: []*udf.OptionValue{
			&udf.OptionValue{
				udf.ValueType_BOOL,
				&udf.OptionValue_BoolValue{false},
			},
		},
	},
}

var udfPoint = &udf.Point{
	Time:            time.Now().UnixNano(),
	Name:            "pointName",
	Database:        "database",
	RetentionPolicy: "policy",
	Group:           "groupId",
	Dimensions:      []string{},
	Tags:            map[string]string{"tag1": "value1", "tag2": "value2"},
	FieldsDouble:    map[string]float64{"cpu_usage": 10.0},
	FieldsInt:       map[string]int64{"queue_size": 10},
	FieldsString:    map[string]string{},
}

func TestHandler(t *testing.T) {
	server, client := fakeConnection(t)
	defer server.Close()
	defer client.Close()

	httpmock.Activate()
	defer httpmock.DeactivateAndReset()
	httpmock.RegisterResponder("GET", RANCHER_URL+"/v1/services/abc",
		httpmock.NewStringResponder(200, `{"id": "abc", "name": "chat", "scale": 1, "transitioning": "no"}`))
	httpmock.RegisterResponder("PUT", RANCHER_URL+"/v1/services/abc",
		httpmock.NewStringResponder(200, `{"id": "abc", "name": "chat", "scale": 2}`))

	setupAgent(t, client)

	server.writeRequest(&udf.Request{Message: &udf.Request_Info{
		Info: &udf.InfoRequest{},
	}})
	server.ReadResponse()

	server.writeRequest(&udf.Request{Message: &udf.Request_Init{
		Init: &udf.InitRequest{Options: options},
	}})
	resp := server.ReadResponse()
	if init := resp.Message.(*udf.Response_Init).Init; !init.Success {
		t.Fatalf("failed to initialize agent: %s", init.Error)
	}
	server.writePoint(udfPoint)
	resp = server.ReadResponse()
	point, ok := resp.Message.(*udf.Response_Point)
	if !ok {
		t.Fatalf("expect to receive a point")
	}
	val := point.Point.GetFieldsInt()["scale"]
	if val != 3 {
		t.Fatalf("expected scale to be 3, got %d", val)
	}
	server.writePoint(udfPoint)
	go func() {
		t.Fatalf("it should not scale up because of cooldown, got '%v'", server.ReadResponse())
	}()
	time.Sleep(time.Millisecond * 10) // no response, good!
}
