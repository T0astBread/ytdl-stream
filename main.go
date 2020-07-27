package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
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
	reason string
	port int
	port_str string
	data_dir string
	cancel context.CancelFunc
}

func StartTor(ctx context.Context, reason string) TorInstance {
	for {
		rand.Seed(time.Now().UnixNano())
		port := 10000 + rand.Intn(500)
		port_str := strconv.Itoa(port)

		fmt.Printf("Starting tor %s (%s)\n", reason, port_str)

		data_dir := "tor_data_" + port_str
		tor_ctx, cancel := context.WithCancel(ctx)
		tor := TorInstance {
			reason,
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
					//fmt.Printf("[tor %s] %s\n", port_str, line)
					if strings.Contains(line, "Bootstrapped 100% (done): Done") {
						fmt.Printf("tor %s (%s) started\n", reason, port_str)
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
	fmt.Printf("stopping tor %s (%s)\n", tor.reason, tor.port_str)
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
	tor = StartTor(ctx, "list-formats")
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
		//fmt.Println("[list-formats]", line)
		parts := strings.SplitN(line, " ", 2)
		if len(parts) > 1 {
			format_id, suffix := parts[0], parts[1]
			//fmt.Println("[list-formats] Format ID:", format_id)
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

func Mkfifo(name string) {
	if err := exec.Command("mkfifo", name).Run(); err != nil {
		panic(err)
	}
}

func RemoveFifo(name string) {
	if err := os.Remove(name); err != nil {
		panic(err)
	}
}

func DownloadFormatTrack(
	ctx context.Context,
	vid_id string,
	format_id string,
	fifo_name string,
	tor TorInstance,
) (TorInstance, error) {
	fifo, err := os.OpenFile(fifo_name, os.O_WRONLY, 0600)
	if err != nil {
		return tor, err
	}
	fifo_writer := bufio.NewWriter(fifo)
	defer fifo.Close()

	for {
		cmd := exec.CommandContext(ctx, "youtube-dl", "--proxy", "socks://127.0.0.1:" + tor.port_str, "-f", format_id, "-o", "-", vid_id)

		out, err := cmd.StdoutPipe()
		if err != nil {
			return tor, err
		}
		reader := io.TeeReader(out, fifo_writer)

		stderr, err := cmd.StderrPipe()
		if err != nil {
			return tor, err
		}
		go func() {
			err_scanner := bufio.NewScanner(stderr)
			for err_scanner.Scan() {
				line := err_scanner.Text()
				fmt.Printf("[download %s, err]", fifo_name, line)
			}
		}()

		if err := cmd.Start(); err != nil {
			// Should we stop?
			select {
				case <-ctx.Done():
					return tor, err
				default:
			}
			tor.Stop()
			tor = StartTor(ctx, "download, start")
		}

		if _, err := ioutil.ReadAll(reader); err != nil {
			return tor, err
		}

		if err := cmd.Wait(); err != nil {
			fmt.Printf("[download %s] %s\n", fifo_name, err)
			// Should we stop?
			select {
				case <-ctx.Done():
					return tor, err
				default:
			}
			tor.Stop()
			tor = StartTor(ctx, "download, wait")
		} else {
			break
		}
	}
	return tor, err
}

func GetTitle(
	ctx context.Context,
	vid_id string,
) string {
	for {
		title, err := _GetTitle(ctx, vid_id)
		title_is_empty := len(title) == 0
		fmt.Println("title_is_empty:", title_is_empty)
		if err != nil || title_is_empty {
			// Should we stop?
			select {
				case <-ctx.Done():
					return title
				default:
			}
			fmt.Println(err)
		} else {
			return title
		}
	}
}

type VidInfo struct {
	Title string `json:title`
}

func _GetTitle(
	ctx context.Context,
	vid_id string,
) (string, error) {
	tor := StartTor(ctx, "title")
	defer tor.Stop()

	ytdl_ctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(ytdl_ctx, "youtube-dl", "--proxy", "socks://127.0.0.1:" + tor.port_str, "--print-json", "-o", "/dev/null", vid_id)

	out, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	//out_scanner := bufio.NewScanner(out)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}
	go func() {
		err_scanner := bufio.NewScanner(stderr)
		for err_scanner.Scan() {
			line := err_scanner.Text()
			fmt.Printf("[title, err]", line)
		}
	}()

	if err := cmd.Start(); err != nil {
		return "", err
	}

	// if !out_scanner.Scan() {
		// println("[title] Got empty output")
		// return "", nil
	// }
	// line := out_scanner.Text()
	// line_reader := strings.NewReader(line)
	vid_info := VidInfo{}
	if err := json.NewDecoder(out).Decode(&vid_info); err != nil {
		return "", err
	}
	fmt.Println("Read title:", vid_info.Title)

	cancel()

	return vid_info.Title, nil
}

func MergeTracks(
	ctx context.Context,
	audio_fifo_name string,
	video_fifo_name string,
	output_file_name string,
) error {
	println("Starting ffmpeg")
	cmd := exec.CommandContext(ctx, "ffmpeg", "-i", video_fifo_name, "-i", audio_fifo_name, output_file_name)

	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	out_scanner := bufio.NewScanner(out)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	go func() {
		err_scanner := bufio.NewScanner(stderr)
		for err_scanner.Scan() {
			line := err_scanner.Text()
			fmt.Printf("[ffmpeg, err]", line)
		}
	}()

	if err := cmd.Start(); err != nil {
		return err
	}

	for out_scanner.Scan() {
		line := out_scanner.Text()
		fmt.Printf("[ffmpeg] %s\n", line)
	}

	return cmd.Wait()
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())

	sig_chan := make(chan os.Signal, 1)
	signal.Notify(sig_chan, os.Interrupt)

	go func() {
		for range sig_chan {
			println("Cancelling")
			cancel()
			return
		}
	}()

	vid_id := os.Args[1]
	vid_format, aud_format, tor := GetBestFormats(ctx, vid_id)

	audio_fifo := "audio_" + vid_id
	Mkfifo(audio_fifo)
	defer RemoveFifo(audio_fifo)

	video_fifo := "video_" + vid_id
	Mkfifo(video_fifo)
	defer RemoveFifo(video_fifo)

	select {
		case <-ctx.Done():
			print("Exited gracefully")
			return
		default:
	}

	fmt.Printf("Best formats: %s/%s\n", vid_format, aud_format)

	go func() {
		println("Starting audio download routine")
		audio_tor, err := DownloadFormatTrack(ctx, vid_id, vid_format, video_fifo, StartTor(ctx, "download, audio-initial"))
		println("Stopping audio download routine")
		audio_tor.Stop()
		if err != nil {
			panic(err)
		}
	}()
	go func() {
		println("Starting video download routine")
		video_tor, err := DownloadFormatTrack(ctx, vid_id, aud_format, audio_fifo, tor)
		println("Stopping video download routine")
		video_tor.Stop()
		if err != nil {
			panic(err)
		}
	}()

	select {
		case <-ctx.Done():
			print("Exited gracefully")
			return
		default:
	}

	title := GetTitle(ctx, vid_id)

	select {
		case <-ctx.Done():
			print("Exited gracefully")
			return
		default:
	}

	err := MergeTracks(ctx, audio_fifo, video_fifo, title + ".mkv")
	if err != nil {
		fmt.Println(err)
	}
}
