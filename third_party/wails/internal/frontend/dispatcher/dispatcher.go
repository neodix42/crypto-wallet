package dispatcher

import (
	"context"
	"fmt"
	"github.com/pkg/errors"
	"github.com/wailsapp/wails/v2/internal/binding"
	"github.com/wailsapp/wails/v2/internal/frontend"
	"github.com/wailsapp/wails/v2/internal/frontend/walletpolicy"
	"github.com/wailsapp/wails/v2/internal/logger"
	"github.com/wailsapp/wails/v2/pkg/options"
)

type Dispatcher struct {
	log                  *logger.Logger
	bindings             *binding.Bindings
	events               frontend.Events
	bindingsDB           *binding.DB
	ctx                  context.Context
	errfmt               options.ErrorFormatter
	disablePanicRecovery bool
}

func NewDispatcher(ctx context.Context, log *logger.Logger, bindings *binding.Bindings, events frontend.Events, errfmt options.ErrorFormatter, disablePanicRecovery bool) *Dispatcher {
	return &Dispatcher{
		log:                  log,
		bindings:             bindings,
		events:               events,
		bindingsDB:           bindings.DB(),
		ctx:                  ctx,
		errfmt:               errfmt,
		disablePanicRecovery: disablePanicRecovery,
	}
}

func (d *Dispatcher) ProcessMessage(message string, sender frontend.Frontend) (_ string, err error) {
	if !d.disablePanicRecovery {
		defer func() {
			if e := recover(); e != nil {
				if errPanic, ok := e.(error); ok {
					err = errPanic
				} else {
					err = fmt.Errorf("%v", e)
				}
			}
			if err != nil {
				err = errors.New("Desktop request failed")
			}
		}()
	}

	if message == "" {
		return "", errors.New("No message to process")
	}
	// Native close-button messages use a separate, non-browser channel.
	if message == "Q" {
		sender.Quit()
		return "", nil
	}
	if _, ok := walletpolicy.BindingCall(message); !ok {
		return "", errors.New("Desktop request unavailable")
	}
	return d.processCallMessage(message, sender)
}
