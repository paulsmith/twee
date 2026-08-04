package netwrap

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
	"time"

	"github.com/paulsmith/twee/third_party/netwrap/internal/netstack"
	"github.com/paulsmith/twee/third_party/netwrap/internal/record"
)

type recordingSink struct {
	recorder *record.Recorder
	warnings io.Writer
	limit    sync.Once
}

func (s *recordingSink) RecordPacket(at time.Time, direction netstack.Direction, packet []byte) error {
	err := s.recorder.RecordPacket(at, record.Direction(direction), packet)
	var limit *record.ErrCaptureLimit
	if errors.As(err, &limit) {
		s.limit.Do(func() {
			fmt.Fprintf(s.warnings, "netwrap: packet capture stopped at the %s byte limit; network forwarding continues\n", strconv.FormatInt(limit.Limit, 10))
		})
		return nil
	}
	return err
}

func (s *recordingSink) RecordFlow(flow netstack.Flow) error {
	return s.recorder.RecordFlow(record.Flow{
		Protocol:            flow.Protocol,
		Direction:           record.Direction(flow.Direction),
		Source:              flow.Source,
		OriginalDestination: flow.OriginalDestination,
		StartTime:           flow.StartTime,
		EndTime:             flow.EndTime,
		Result:              flow.Result,
		Error:               flow.Error,
		BytesSent:           flow.BytesSent,
		BytesReceived:       flow.BytesReceived,
	})
}
