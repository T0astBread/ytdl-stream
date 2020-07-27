package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"os/signal"
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

func StartTor(ctx context.Context) TorInstance {
	for {
		rand.Seed(time.Now().UnixNano())
		port := 10000 + rand.Intn(500)
		port_str := strconv.Itoa(port)

		data_dir := "tor_data_" + port_str
		tor_ctx, cancel := context.WithCancel(ctx)
		tor := TorInstance {
			port,
			port_str,
			data_dir,
			cancel,
		}

		cmd := exec.CommandContext(tor_ctx, "tor", "--HTTPTunnelPort", "0", "--SocksPort", port_str, "--DataDirectory", data_dir)

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

		// Are we done?
		select {
			case <-ctx.Done():
				return tor
			default:
		}
	}
}

func (tor TorInstance) Stop() {
	tor.cancel()
	os.RemoveAll(tor.data_dir)
}

func GetBestFormats(
	ctx context.Context,
	vid_id string,
) (vid_format string, aud_format string, tor TorInstance) {
	for {
		vid_format, aud_format, tor, err := _GetBestFormats(ctx, vid_id)
		if err == nil {
			return vid_format, aud_format, tor
		} else {
			println(err)
			select {
				case <-ctx.Done():
					return vid_format, aud_format, tor
				default:
			}
		}
	}
}

func _GetBestFormats(
	ctx context.Context,
	vid_id string,
) (vid_format string, aud_format string, tor TorInstance, err error) {
	tor = StartTor(ctx)
	defer func() {
		if err != nil {
			tor.Stop()
		}
	}()

	cmd := exec.CommandContext(ctx, "youtube-dl", "--proxy", "socks://127.0.0.1:" + tor.port_str, "--list-formats", vid_id)
	out, _err := cmd.StdoutPipe()
	if _err != nil {
		err = _err
		return
	}
	out_scanner := bufio.NewScanner(out)


	stderr, _err := cmd.StderrPipe()
	if _err != nil {
		err = _err
		return
	}
	go func() {
		err_scanner := bufio.NewScanner(stderr)
		for err_scanner.Scan() {
			line := err_scanner.Text()
			fmt.Printf("[list-formats, err]", line)
		}
	}()

	if _err := cmd.Start(); _err != nil {
		err = _err
		return
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

	if _err := cmd.Wait(); _err != nil {
		err = _err
		return
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
	ctx, cancel := context.WithCancel(context.Background())

	sig_chan := make(chan os.Signal, 1)
	signal.Notify(sig_chan, os.Interrupt)

	go func() {
		for range sig_chan {
			cancel()
			return
		}
	}()

	vid_format, aud_format, tor := GetBestFormats(ctx, os.Args[1])

	select {
		case <-ctx.Done():
			print("Exited gracefully")
			return
		default:
	}

	println("Best formats:")
	println(vid_format)
	println(aud_format)
	println("Tor port:")
	println(tor.port_str)
	tor.Stop()
}
