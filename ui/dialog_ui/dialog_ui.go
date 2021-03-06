package dialog_ui

import (
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/dorzheh/deployer/utils"
	. "github.com/dorzheh/go-dialog"
)

const (
	DialogExit     = "exit status 1"
	DialogMoveBack = "exit status 2"
	DialogNext     = "exit status 3"
)

const (
	Success      = "Success"
	Error        = "Failure"
	Warning      = "Warning"
	Notification = "Notification"
	None         = ""
)

type Pb struct {
	sleep time.Duration
	step  int
}

func (p *Pb) SetSleep(s string) (err error) {
	p.sleep, err = time.ParseDuration(s)
	err = utils.FormatError(err)
	return
}

func (p *Pb) SetStep(s int) {
	p.step = s
}

func (p *Pb) Sleep() time.Duration {
	return p.sleep
}

func (p *Pb) Step() int {
	return p.step
}

func (p *Pb) IncreaseSleep(s string) error {
	sleep, err := time.ParseDuration(s)
	if err != nil {
		return utils.FormatError(err)
	}
	p.sleep += sleep
	return nil
}

func (p *Pb) IncreaseStep(s int) {
	p.step += s
}

func (p *Pb) DecreaseStep(s int) {
	p.step -= s
}

type DialogUi struct {
	*Dialog
	Pb *Pb
}

func NewDialogUi() *DialogUi {
	return &DialogUi{New(CONSOLE, 0), &Pb{0, 0}}
}

///// Functions providing verification services /////

// Output gets dialog session , height/width and slice of strings
// Prints out appropriate message
func (ui *DialogUi) Output(ntype string, msgs ...string) {
	msg, height, width := getMsgAndWidth(ntype, msgs...)
	ui.SetSize(height, width)
	ui.SetTitle(ntype)
	if ntype == Error {
		ui.Infobox(msg)
		os.Exit(1)
	}
	ui.Msgbox(msg)
}

///// Functions for the progress bar implementation /////

// WaitForCmdToFinish prints a progress bar upon a command execution
// It gets a dialog session, command to execute,
// title for progress bar and the time duration
// Returns error
// func (ui *DialogUi) WaitForCmdToFinish(cmd *exec.Cmd, title, msg string, step int, duration time.Duration) error {
// 	// execute the command in a background
// 	err := cmd.Start()
// 	if err != nil {
// 		return utils.FormatError(err)
// 	}
// 	// allocate a channel
// 	done := make(chan error)
// 	go func() {
// 		// wait in background until the command has make it's job
// 		done <- cmd.Wait()
// 	}()
// 	// show progress bar for a while
// 	return ui.Progress(title, msg, duration, step, done)
// }

// Progress implements a progress bar
// Returns error or nil
func (ui *DialogUi) Progress(title, pbMsg string, duration time.Duration, step int, done chan error) error {
	defaultWidth := 50
	titleWidth := len(title) + 4
	msgWidth := len(pbMsg) + 4
	var newWidth int
	if titleWidth > msgWidth {
		newWidth = titleWidth
	} else {
		newWidth = msgWidth
	}
	if defaultWidth > newWidth {
		newWidth = defaultWidth
	}
	ui.SetTitle(title)
	ui.SetSize(8, newWidth)
	pb := ui.Progressbar()
	var interval int = 0
	for {
		select {
		// wait for result
		case result := <-done:
			if result != nil {
				return result
			}
			// we are finished - 100% done
			pb.Step(100, "\n\nDone!")
			ui.SetSize(6, 15)
			finalSleep, err := time.ParseDuration("1s")
			if err != nil {
				return utils.FormatError(err)
			}
			time.Sleep(finalSleep)
			return nil
		default:
			if interval < 100 {
				interval += step
			}
			if interval > 100 {
				interval = 100
			}
			pb.Step(interval, pbMsg)
			time.Sleep(duration)
		}
	}
	return nil
}

// Wait communicates with a progress bar while a given function is executed
// Returns error or nil
func (ui *DialogUi) Wait(msg string, pause, timeOut time.Duration, done chan error) error {
	ui.SetSize(6, 55)
	ui.Infobox(msg)
	t := time.After(timeOut)
	for {
		select {
		// wait for result
		case result := <-done:
			return result
		// Timeout is reached
		case <-t:
			if timeOut > 0 {
				return errors.New("Timeout was reached")
			}
		default:
			time.Sleep(pause)
		}
	}
	return nil
}

// GetPathToFileFromInput uses a dialog session for getting path to a file
func (ui *DialogUi) GetPathToFileFromInput(title, helpButtonLabel, extraButtonLabel string) (string, error) {
	var result string
	var err error

	for {
		ui.SetTitle(title)
		ui.SetSize(10, 50)
		if helpButtonLabel != "" {
			ui.HelpButton(true)
			ui.SetHelpLabel(helpButtonLabel)
		}
		if extraButtonLabel != "" {
			ui.SetExtraLabel(extraButtonLabel)
		}
		result, err = ui.Fselect("/")
		if err != nil {
			return result, err
		}
		if result != "" {
			stat, err := os.Stat(result)
			if err == nil && !stat.IsDir() {
				break
			}
		}
	}
	return result, nil
}

// GetPathToDirFromInput uses a dialog session for getting path to a directory to upload
func (ui *DialogUi) GetPathToDirFromInput(title, defaultDir, helpButtonLabel, extraButtonLabel string) (string, error) {
	var result string
	var err error

	for {
		ui.SetTitle(title)
		ui.SetSize(10, 50)
		if helpButtonLabel != "" {
			ui.HelpButton(true)
			ui.SetHelpLabel(helpButtonLabel)
		}
		if extraButtonLabel != "" {
			ui.SetExtraLabel(extraButtonLabel)
		}
		result, err = ui.Dselect(defaultDir)
		if err != nil {
			return result, err
		}
		if result != "" {
			stat, err := os.Stat(result)
			if err == nil && stat.IsDir() {
				break
			}
		}
	}
	return result, nil
}

// GetIpFromInput uses a dialog session for reading IP from user input
// Returns host IP (remote or local)
func (ui *DialogUi) GetIpFromInput(title, helpButtonLabel, extraButtonLabel string) (string, error) {
	var ipAddr string
	var err error

	width := len(title) + 7
	for {
		ui.SetSize(8, width)
		ui.SetTitle(title)
		if helpButtonLabel != "" {
			ui.HelpButton(true)
			ui.SetHelpLabel(helpButtonLabel)
		}
		if extraButtonLabel != "" {
			ui.SetExtraLabel(extraButtonLabel)
		}
		ipAddr, err = ui.Inputbox("")
		if err != nil {
			return ipAddr, err
		}
		// validate the IP
		if net.ParseIP(ipAddr) == nil {
			ui.Output(Warning, "Invalid IP.", "Press <OK> to return to menu.")
			continue
		}
		break
	}
	return ipAddr, nil
}

// GetFromInput uses a dialog session for reading from stdin
// Returns user input
func (ui *DialogUi) GetFromInput(title, defaultInput, helpButtonLabel, extraButtonLabel string) (string, error) {
	var input string
	var err error

	for {
		ui.SetSize(8, len(title)+5)
		ui.SetTitle(title)
		if helpButtonLabel != "" {
			ui.HelpButton(true)
			ui.SetHelpLabel(helpButtonLabel)
		}
		if extraButtonLabel != "" {
			ui.SetExtraLabel(extraButtonLabel)
		}
		input, err = ui.Inputbox(defaultInput)
		if err != nil {
			return input, err
		}
		if input != "" {
			break
		}
	}
	return input, nil
}

//GetPasswordFromInput uses a dialog session for reading user password from user input
//Returns password string
func (ui *DialogUi) GetPasswordFromInput(host, user, helpButtonLabel, extraButtonLabel string, confirm bool) (passwd1 string, err error) {
MainLoop:
	for {
		msg := fmt.Sprintf("\"%s\" password on the host %s", user, host)
		for {
			ui.SetSize(8, len(msg)+5)
			ui.SetTitle(msg)
			if helpButtonLabel != "" {
				ui.HelpButton(true)
				ui.SetHelpLabel(helpButtonLabel)
			}
			if extraButtonLabel != "" {
				ui.SetExtraLabel(extraButtonLabel)
			}
			passwd1, err = ui.Passwordbox(true)
			if err != nil {
				return "", err
			}
			if passwd1 != "" {
				return
			}
		}
		if confirm {
			var passwd2 string
			msg = "Password confirmation for user \"" + user + "\""
			for {
				if extraButtonLabel != "" {
					ui.SetExtraLabel(extraButtonLabel)
				}
				ui.HelpButton(true)
				ui.SetHelpLabel("Back")
				ui.SetSize(8, len(msg)+5)
				ui.SetTitle(msg)
				ui.HelpButton(true)
				ui.SetHelpLabel("Back")
				passwd2, err = ui.Passwordbox(true)
				switch err.Error() {
				case DialogMoveBack:
					continue MainLoop
				default:
					return
				}
				if passwd2 != "" {
					break
				}
			}
			if passwd1 == passwd2 {
				return
			}
		}
	}
	return
}

func getMsgAndWidth(mtype string, stroki ...string) (string, int, int) {
	var msg string
	height := 6
	width := 0
	for _, str := range stroki {
		strLength := len(str)
		if strLength > width {
			width = strLength
		}
		height++
		msg += "\n" + str

	}
	return msg + "\n", height, width + 5
}
