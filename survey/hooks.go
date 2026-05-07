package survey

type SchedulingHooks struct {
	ScheduleAutoAIMessages       func(string) error
	ScheduleAutoFollowupMessages func(string) error
	DeletePendingFollowup        func(string, string) error
	AfterBaselineCompleted       func(string) error
}

var schedulingHooks SchedulingHooks

func SetSchedulingHooks(h SchedulingHooks) {
	schedulingHooks = h
}
