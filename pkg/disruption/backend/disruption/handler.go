package disruption

import (
	"fmt"

	"github.com/openshift/origin/pkg/disruption/backend"
	"github.com/openshift/origin/pkg/monitor/backenddisruption"
	"github.com/openshift/origin/pkg/monitor/monitorapi"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/events"
	"k8s.io/kubernetes/test/e2e/framework"
)

// newCIHandler returns a new intervalHandler instance
// that can record the availability and unavailability
// interval in CI using the Monitor API and the event handler.
//
//	monitor: Monitor API to start and end an interval in CI
//	eventRecorder: to create events associated with the intervals
//	locator: the CI locator assigned to this disruption test
//	name: name of the disruption test
//	connType: user specified BackendConnectionType used in this test
func newCIHandler(descriptor backend.TestDescriptor, monitor backend.Monitor, eventRecorder events.EventRecorder) *ciHandler {
	return &ciHandler{
		descriptor:     descriptor,
		monitor:        monitor,
		eventRecorder:  eventRecorder,
		openIntervalID: -1,
	}
}

var _ intervalHandler = &ciHandler{}
var _ backend.WantEventRecorderAndMonitor = &ciHandler{}

// ciHandler records the availability and unavailability interval in CI
type ciHandler struct {
	descriptor    backend.TestDescriptor
	monitor       backend.Monitor
	eventRecorder events.EventRecorder

	openIntervalID int
	last           backend.SampleResult
}

// SetEventRecorder sets the event recorder
func (h *ciHandler) SetEventRecorder(recorder events.EventRecorder) {
	h.eventRecorder = recorder
}

// SetMonitor sets the interval recorder provided by the monitor API
func (h *ciHandler) SetMonitor(monitor backend.Monitor) {
	h.monitor = monitor
}

// Unavailable is called for a disruption interval when we see
// a series of failed samples in this range [from ... to).
//
//	a) either from or to must not be nil
//	b) for a window with a single sample, from and to can refer
//	   to the same sample in question.
func (h *ciHandler) Unavailable(from, to *backend.SampleResult) {
	fs, ts := from.Sample, to.Sample
	info := fmt.Sprintf("sample-id=%d %s", fs.ID, from.String())
	if ts.ID-fs.ID > 1 {
		// we have multiple failed samples with the same error
		info = fmt.Sprintf("range=[%d-%d] %s", fs.ID, ts.ID, info)
	}
	message, eventReason, level := backenddisruption.DisruptionBegan(h.descriptor.DisruptionLocator(),
		h.descriptor.GetConnectionType(), fmt.Errorf("%w - %s", from.AggregateErr(), info), "no-audit-id")

	framework.Logf(message)
	h.eventRecorder.Eventf(
		&v1.ObjectReference{Kind: "OpenShiftTest", Namespace: "kube-system", Name: h.descriptor.Name()},
		nil, v1.EventTypeWarning, string(eventReason), "detected", message)

	condition := monitorapi.Condition{
		Level:   level,
		Locator: h.descriptor.DisruptionLocator(),
		Message: message,
	}
	openIntervalID := h.monitor.StartInterval(fs.StartedAt, condition)
	// TODO: unlikely in the real world, if from == to for some reason,
	//  then we are recording a zero second unavailable window.
	h.monitor.EndInterval(openIntervalID, ts.StartedAt)
}

// Available is called when a disruption interval ends and we see
// a series of successful samples in this range [from ... to).
//
//	a) either from or to must not be nil
//	b) for a window with a single sample, from and to can refer
//	   to the same sample in question.
func (h *ciHandler) Available(from, to *backend.SampleResult) {
	fs, ts := from.Sample, to.Sample
	message := backenddisruption.DisruptionEndedMessage(h.descriptor.DisruptionLocator(), h.descriptor.GetConnectionType())
	framework.Logf(message)

	h.eventRecorder.Eventf(
		&v1.ObjectReference{Kind: "OpenShiftTest", Namespace: "kube-system", Name: h.descriptor.Name()}, nil,
		v1.EventTypeNormal, string(monitorapi.DisruptionEndedEventReason), "detected", message)
	condition := monitorapi.Condition{
		Level:   monitorapi.Info,
		Locator: h.descriptor.DisruptionLocator(),
		Message: message,
	}
	openIntervalID := h.monitor.StartInterval(fs.StartedAt, condition)
	h.monitor.EndInterval(openIntervalID, ts.StartedAt)
}
