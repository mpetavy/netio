package main

import (
	"flag"
	"fmt"
	"github.com/mpetavy/common"
	"io/ioutil"
	"net"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var test = flag.Bool("test", false, "Run tests")
var testDevices = flag.String("test.devices", "", "Two TTY devices to run transfer tests separated by a dash (i.e. \"COM3-COM3\"")

func TryMain() error {
	cmd := exec.Command("go", "build")

	err := cmd.Run()
	if common.Error(err) {
		return err
	}

	tries := []struct {
		description string
		start       time.Time
		end         time.Time
		cmd         []string
		observed    int
		err         error
	}{
		{
			description: fmt.Sprintf("Testing network forward transfer"),
			cmd: []string{
				"netio -s :9999 -tls -e dc70cc028aadfad54b0a587b6f10b833 -e 0535df82c4749f4af18f07c8fbae8ef7 -e 736b945073b56d09c163ce7f2ee98ac5",
				"netio -c :9999 -tls -f test1.txt -f test2.txt -f test3.txt"},
			observed: 0,
		},
		{
			description: fmt.Sprintf("Testing network backward transfer"),
			cmd: []string{
				"netio -s :9999 -tls -ds -f test1.txt -f test2.txt -f test3.txt",
				"netio -c :9999 -tls -dr -e dc70cc028aadfad54b0a587b6f10b833 -e 0535df82c4749f4af18f07c8fbae8ef7 -e 736b945073b56d09c163ce7f2ee98ac5"},
			observed: 1,
		},
		{
			description: fmt.Sprintf("Testing serial forward transfer"),
			cmd: []string{
				"netio -s COM3,115200 -e dc70cc028aadfad54b0a587b6f10b833 -e 0535df82c4749f4af18f07c8fbae8ef7 -e 736b945073b56d09c163ce7f2ee98ac5",
				"netio -c COM4,115200 -f test1.txt -f test2.txt -f test3.txt"},
			observed: 1,
		},
		{
			description: fmt.Sprintf("Testing serial nackward transfer"),
			cmd: []string{
				"netio -s COM3,115200 -e dc70cc028aadfad54b0a587b6f10b833 -e 0535df82c4749f4af18f07c8fbae8ef7 -e 736b945073b56d09c163ce7f2ee98ac5",
				"netio -c COM4,115200 -f test1.txt -f test2.txt -f test3.txt"},
			observed: 1,
		},
	}

	allTriesSucceded := true

	for i := 0; i < len(tries); i++ {
		tries[i].start = time.Now()
		tries[i].err = startTest()
		tries[i].end = time.Now()

		allTriesSucceded = allTriesSucceded && (tries[i].err == nil || strings.Index(tries[i].err.Error(), "skip") != -1)
	}

	common.Info("Finished running tests, results:")

	page, err := common.NewPage(nil, "", common.TitleVersion(true, true, true))
	if common.Error(err) {
		return err
	}

	_, hostname, err := common.GetHost()
	if common.Error(err) {
		hostname = "<na>"
	}

	addrs, err := common.GetHostAddrs(true, nil)

	ips := make([]string, 0)

	for _, addr := range addrs {
		ip, _, err := net.ParseCIDR(addr.Addr.String())
		if common.Error(err) {
			return err
		}

		ips = append(ips, ip.String())
	}

	systemInfo, err := common.GetSystemInfo()
	if common.Error(err) {
		return err
	}

	rows := make([][]string, 0)

	rows = append(rows, []string{common.Translate("Settings"), strings.Title(common.Translate("Value"))})
	rows = append(rows, []string{common.Translate("Title"), strings.Title(common.Title())})
	rows = append(rows, []string{common.Translate("Version"), strings.Title(common.App().Version)})
	rows = append(rows, []string{common.Translate("Build git"), LDFLAG_GIT})
	rows = append(rows, []string{common.Translate("Build counter"), LDFLAG_COUNTER})

	table := common.NewTable(page.HtmlContent.CreateElement("center"), rows)
	table.CreateAttr("border", "1")

	page.HtmlContent.CreateElement("br")

	rows = make([][]string, 0)

	rows = append(rows, []string{common.Translate("Testserver attribute"), strings.Title(common.Translate("Value"))})
	rows = append(rows, []string{common.Translate("Host name"), hostname})
	rows = append(rows, []string{common.Translate("Network ip's"), strings.Join(ips, "??br")})
	rows = append(rows, []string{common.Translate("OS"), fmt.Sprintf("%s %s %s", systemInfo.KernelName, systemInfo.KernelVersion, systemInfo.KernelRelease)})
	rows = append(rows, []string{common.Translate("CPU architecture"), strings.Join([]string{runtime.GOARCH, fmt.Sprintf("%d Cores", runtime.NumCPU()), systemInfo.Platform}, " / ")})
	rows = append(rows, []string{common.Translate("Routines running"), strconv.Itoa(runtime.NumGoroutine())})
	rows = append(rows, []string{common.Translate("RAM free / total"), strings.Join([]string{systemInfo.MemFree, systemInfo.MemTotal}, " / ")})

	table = common.NewTable(page.HtmlContent.CreateElement("center"), rows)
	table.CreateAttr("border", "1")

	page.HtmlContent.CreateElement("br")

	rows = make([][]string, 0)

	rows = append(rows, []string{
		common.Translate("Description"),
		common.Translate("Test"),
		common.Translate("Start time"),
		common.Translate("End time"),
		common.Translate("Result")})

	for i := 0; i < len(tries); i++ {
		var result string

		switch {
		case tries[i].start.IsZero():
			result = "SKIPPED"
		case tries[i].err == nil:
			result = "SUCCESS"
		case tries[i].err != nil:
			if strings.Contains(tries[i].err.Error(), "skip") {
				result = fmt.Sprintf("SKIPPED: %+v", tries[i].err)
			} else {
				result = fmt.Sprintf("FAILED: %+v", tries[i].err)
			}
		}

		common.Info("%s: %s", tries[i].description, result)

		rows = append(rows, []string{
			tries[i].description,
			fmt.Sprintf("%v", tries[i].start.Format(common.DateTimeMask)),
			fmt.Sprintf("%v", tries[i].end.Format(common.DateTimeMask)),
			fmt.Sprintf("%v", result)})
	}

	table = common.NewTable(page.HtmlContent.CreateElement("center"), rows)
	table.CreateAttr("border", "1")

	html, err := page.HTML()
	if common.Error(err) {
		return err
	}

	fn := common.CleanPath(common.AppFilename("-tests.html"))

	common.Info("Save test results: %s", fn)

	err = ioutil.WriteFile(fn, []byte(html), common.DefaultFileMode)
	if common.Error(err) {
		return err
	}

	if err == nil && !allTriesSucceded {
		err = fmt.Errorf("some tests failed")
	}

	return err
}

func startTest() error {
	return nil
}
