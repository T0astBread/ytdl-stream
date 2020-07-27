package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type TorInstance struct {
	port int
	port_str string
	data_dir string
	cancel context.CancelFunc
}

func StartTor() TorInstance {
	for {
		rand.Seed(time.Now().UnixNano())
		port := 10000 + rand.Intn(500)
		port_str := strconv.Itoa(port)

		data_dir := "tor_data_" + port_str
		ctx, cancel := context.WithCancel(context.Background())
		tor := TorInstance {
			port,
			port_str,
			data_dir,
			cancel,
		}

		cmd := exec.CommandContext(ctx, "tor", "--HTTPTunnelPort", "0", "--SocksPort", port_str, "--DataDirectory", data_dir)

		out, err := cmd.StdoutPipe()
		if err != nil {
			log.Fatal(err)
		} else {
			out_scanner := bufio.NewScanner(out)

			if err := cmd.Start(); err != nil {
				tor.Stop()
			} else {
				for out_scanner.Scan() {
					line := out_scanner.Text()
					fmt.Printf("[tor %s] %s\n", port_str, line)
					if strings.Contains(line, "Bootstrapped 100% (done): Done") {
						return tor
					}
				}
			}
		}
	}
}

func (tor TorInstance) Stop() {
	tor.cancel()
	os.RemoveAll(tor.data_dir)
}

func GetBestFormats(vid_id string) (vid_format string, aud_format string) {
	for {
		vid_format, aud_format, err := _GetBestFormats(vid_id)
		if err == nil {
			return vid_format, aud_format
		} else {
			println(err)
		}
	}
}

func _GetBestFormats(vid_id string) (vid_format string, aud_format string, err error) {
	tor := StartTor()
	defer tor.Stop()

	cmd := exec.CommandContext(context.Background(), "youtube-dl", "--proxy", "socks://127.0.0.1:" + tor.port_str, "--list-formats", vid_id)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return vid_format, aud_format, err
	}
	out_scanner := bufio.NewScanner(out)


	stderr, err := cmd.StderrPipe()
	if err != nil {
		return vid_format, aud_format, err
	}
	go func() {
		err_scanner := bufio.NewScanner(stderr)
		for err_scanner.Scan() {
			line := err_scanner.Text()
			fmt.Printf("[list-formats, err]", line)
		}
	}()

	if err := cmd.Start(); err != nil {
		return vid_format, aud_format, err
	}

	best_vid_value, best_aud_value := -1, -1
	for out_scanner.Scan() {
		line := out_scanner.Text()
		fmt.Println("[list-formats]", line)
		parts := strings.SplitN(line, " ", 2)
		if len(parts) > 1 {
			format_id, suffix := parts[0], parts[1]
			fmt.Println("[list-formats] Format ID:", format_id)
			if strings.Contains(suffix, "audio only") {
				format_value := AudioFormatValue(format_id)
				if format_value > best_aud_value {
					aud_format = format_id
					best_aud_value = format_value
				}
			} else if strings.Contains(suffix, "video only") {
				format_value := VideoFormatValue(format_id)
				if format_value > best_vid_value {
					vid_format = format_id
					best_vid_value = format_value
				}
			}
		}
	}

	if err := cmd.Wait(); err != nil {
		return vid_format, aud_format, err
	}

	return
}

func AudioFormatValue(format string) int {
	switch format {
		case "249": return 0
		case "250": return 1
		case "251": return 2
		case "140": return 3
		default: return -1
	}
}

func VideoFormatValue(format string) int {
	switch format {
		case "160": return 0
		case "278": return 1
		case "242": return 2
		case "133": return 3
		case "243": return 4
		case "134": return 5
		case "244": return 6
		case "135": return 7
		case "247": return 8
		case "136": return 9
		case "302": return 10
		case "298": return 11
		case "248": return 12
		case "137": return 13
		case "303": return 14
		case "299": return 15
		default: return -1
	}
}

func main() {
	vid_format, aud_format := GetBestFormats(os.Args[1])
	println("Best formats:")
	println(vid_format)
	println(aud_format)
}
