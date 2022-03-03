package tasklet

import (
	"fmt"

	"github.com/nitrictech/cli/pkg/output"
	"github.com/pterm/pterm"
)

var defaultSequence = []string{"⠟", "⠯", "⠷", "⠾", "⠽", "⠻"}

type TaskletContext interface {
	output.Progress
	Start() error
	Stop() error
}

type taskletSpinnerContext struct {
	spinner *pterm.SpinnerPrinter
}

var _ output.Progress = &taskletSpinnerContext{}

func (c *taskletSpinnerContext) Start() error {
	var err error
	c.spinner, err = c.spinner.Start()
	return err
}

func (c *taskletSpinnerContext) Stop() error {
	return c.spinner.Stop()
}

func (c *taskletSpinnerContext) Debugf(format string, a ...interface{}) {
	pterm.Debug.Printf(format, a...)
}

func (c *taskletSpinnerContext) Busyf(format string, a ...interface{}) {
	c.spinner.UpdateText(fmt.Sprintf(format, a...))
}

func (c *taskletSpinnerContext) Successf(format string, a ...interface{}) {
	c.spinner.SuccessPrinter.Printf(format, a...)
}

func (c *taskletSpinnerContext) Failf(format string, a ...interface{}) {
	pterm.Error.Printf(format, a...)
}

func NewSpinnerContext(startMsg string) *taskletSpinnerContext {
	return &taskletSpinnerContext{
		spinner: pterm.DefaultSpinner.WithShowTimer().WithSequence(defaultSequence...).WithText(startMsg),
	}
}

type taskletAreaContext struct {
	area *pterm.AreaPrinter
}

var _ output.Progress = &taskletSpinnerContext{}

func (c *taskletAreaContext) Start() error {
	var err error
	c.area, err = c.area.Start()
	return err
}

func (c *taskletAreaContext) Stop() error {
	return c.area.Stop()
}

func (c *taskletAreaContext) Debugf(format string, a ...interface{}) {
	pterm.Debug.Printf(format, a...)
}

func (c *taskletAreaContext) Busyf(format string, a ...interface{}) {
	c.area.Update(fmt.Sprintf(format, a...))
}

func (c *taskletAreaContext) Successf(format string, a ...interface{}) {
	pterm.Success.Printf(format, a...)
}

func (c *taskletAreaContext) Failf(format string, a ...interface{}) {
	pterm.Error.Printf(format, a...)
}

func NewAreaContext() (*taskletAreaContext, error) {
	area := pterm.DefaultArea.WithRemoveWhenDone(false)

	return &taskletAreaContext{
		area: area,
	}, nil
}
