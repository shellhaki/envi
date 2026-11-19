package cli

// Which Envi `envi auth` signs in to: Envi Cloud or your own instance. Asked on every envi auth in a terminal;
// --cloud or --url answer it ahead, and ENVI_API_URL overrides it.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// chooseServer settles the server before signing in, saving the choice the way envi set-url does.
func (a App) chooseServer(store TokenStore, cloud bool, instanceURL string, in io.Reader) error {
	if cloud && instanceURL != "" {
		return errors.New("choose --cloud or --url, not both")
	}
	if os.Getenv("ENVI_API_URL") != "" {
		if cloud || instanceURL != "" {
			return errors.New("ENVI_API_URL is set, and it decides the server; unset it to use --cloud or --url")
		}
		return nil
	}
	saved := savedAPIURL()
	if !cloud && instanceURL == "" {
		if !isTerminal(a.In) {
			return nil
		}
		var err error
		if cloud, instanceURL, err = askServer(in, a.Out, saved); err != nil {
			return err
		}
	}
	o := SetURLOptions{Version: a.Version, SigningIn: true}
	if cloud {
		if saved == "" {
			return nil
		}
		o.Reset = true
	} else {
		next, err := NormalizeAPIURL(instanceURL)
		if err != nil {
			return err
		}
		if next == saved {
			return nil
		}
		o.URL = next
	}
	return SetAPIURL(context.Background(), o, store, a.Out)
}

// askServer asks where to sign in. The default is the server in use now.
func askServer(in io.Reader, out io.Writer, saved string) (cloud bool, instanceURL string, err error) {
	ui := NewUI(out)
	r := bufio.NewReader(in)
	choice := "1"
	if saved != "" {
		choice = "2"
	}
	ui.Print("Where do you sign in?")
	ui.Print("  1) Envi Cloud           %s", ui.Dim("cloud.envisecrets.com"))
	if saved != "" {
		ui.Print("  2) Your own instance    %s", ui.Dim(saved))
	} else {
		ui.Print("  2) Your own instance    %s", ui.Dim("a self-hosted Envi"))
	}
	fmt.Fprintf(out, "Choose [%s]: ", choice)
	answer, err := readLine(r)
	if err != nil {
		return false, "", err
	}
	if answer != "" {
		choice = answer
	}
	switch choice {
	case "1":
		return true, "", nil
	case "2":
	default:
		return false, "", fmt.Errorf("%q isn't 1 or 2", choice)
	}
	if saved != "" {
		fmt.Fprintf(out, "Instance API URL [%s]: ", saved)
	} else {
		fmt.Fprint(out, "Instance API URL (e.g. https://api.envi.example.com): ")
	}
	instanceURL, err = readLine(r)
	if err != nil {
		return false, "", err
	}
	if instanceURL == "" {
		instanceURL = saved
	}
	if instanceURL == "" {
		return false, "", errors.New("an instance API URL is needed; envi-instance urls on the server shows it")
	}
	return false, instanceURL, nil
}

func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func isTerminal(in io.Reader) bool {
	f, ok := in.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}
