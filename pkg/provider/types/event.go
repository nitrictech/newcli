package types

type Event struct {
	Result   *ResultMessage
	Progress *ProgressMessage
}

type ResultMessage struct {
	Content string
	Success bool
}

type ProgressMessage struct {
	Content string
}
