package agent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/xmigrate/blxrep/utils"

	"github.com/gorilla/websocket"
)

func Clone(ctx context.Context, blockSize int, srcPath string, channelSize int, websock *websocket.Conn, cloneMutex *sync.Mutex, isCloning *bool) {

	// Open the source disk.
	src, err := os.Open(srcPath)
	if err != nil {
		log.Printf("Failed to open source disk: %v", err)
	}
	defer src.Close()

	// Use a buffered reader to minimize system calls.
	bufReader := bufio.NewReaderSize(src, blockSize*8000)

	// Allocate a buffer for one block.
	buf := make([]byte, blockSize)

	var blocks []utils.AgentDataBlock
	var blockCount uint64
	var batchSize int
	log.Printf("Cloning started for %s", srcPath)
	startTime := time.Now().Unix()
	for {
		select {
		case <-ctx.Done():
			// Handle context cancellation and exit the goroutine
			log.Println("Cloning was paused/cancelled and goroutine is exiting.")
			if len(blocks) > 0 {
				utils.StreamData(blocks, websock, false, srcPath, utils.CONST_AGENT_ACTION_CLONE, startTime)
			}
			cloneMutex.Lock()
			*isCloning = false
			cloneMutex.Unlock()
			return
		default:
			// Read data in larger chunks to reduce syscall overhead
			n, err := bufReader.Read(buf)
			if n > 0 {
				for i := 0; i < n; i += blockSize {
					end := i + blockSize
					if end > n {
						end = n
					}
					blockData := utils.AgentDataBlock{
						BlockNumber: blockCount,
						BlockData:   append([]byte(nil), buf[i:end]...),
					}
					blocks = append(blocks, blockData)
					blockCount++
					batchSize += end - i

					if batchSize >= channelSize {
						utils.StreamData(blocks, websock, false, srcPath, utils.CONST_AGENT_ACTION_CLONE, startTime)
						blocks = nil
						batchSize = 0
					}
				}
			}
			if err != nil {
				if err == io.EOF {
					if len(blocks) > 0 {
						utils.StreamData(blocks, websock, false, srcPath, utils.CONST_AGENT_ACTION_CLONE, startTime)
					}
					cloneMutex.Lock()
					*isCloning = false
					cloneMutex.Unlock()
					return
				}
				log.Fatalf("Failed to read block: %v", err)
			}
		}
	}
}

func Resume(ctx context.Context, blockSize int, srcPath string, channelSize int, readFrom int64, websock *websocket.Conn, cloneMutex *sync.Mutex, isCloning *bool) {
	log.Printf("Resume started block: %d", readFrom)
	// Open the source disk.
	src, err := os.Open(srcPath)
	if err != nil {
		log.Printf("Failed to open source disk: %v", err)
	}
	defer src.Close()
	var blocks []utils.AgentDataBlock

	// Loop over the blocks in the source disk.
	var blockCount int64 = 0 // Initialize counter to 0
	var batchSize int = 0
	// Seek to correct block number
	for {
		_, err := src.Seek(int64(blockSize), io.SeekCurrent)
		if err != nil && err != io.EOF {
			fmt.Println("Error reading from snapshot:", err)
			return
		}

		if blockCount == readFrom {
			log.Printf("Seeked to %d", blockCount)
			break
		}
		blockCount++
	}
	// Loop over the blocks in the source disk.
	startTime := time.Now().Unix()
	for {
		select {
		case <-ctx.Done():
			// Handle context cancellation and exit the goroutine
			log.Println("Cloning was paused/cancelled and goroutine is exiting.")
			if len(blocks) > 0 {
				utils.StreamData(blocks, websock, true, srcPath, utils.CONST_AGENT_ACTION_CLONE, startTime)
				utils.LogDebug(fmt.Sprintf("Flush remaining data of size %d", batchSize))
			}
			return
		default:
			var bytesRead int
			buf := make([]byte, blockSize)
			for bytesRead < blockSize {
				n, err := src.Read(buf[bytesRead:])
				if n > 0 {
					bytesRead += n
				}
				if err != nil {
					if err == io.EOF {
						break
					}
					log.Fatalf("Failed to read block: %v", err)
				}
			}
			if bytesRead > 0 {
				blockData := utils.AgentDataBlock{
					BlockNumber: uint64(blockCount),
					BlockData:   append([]byte(nil), buf[:bytesRead]...),
				}
				blocks = append(blocks, blockData)
				if batchSize >= channelSize {
					//Code to send data to websocket
					utils.StreamData(blocks, websock, true, srcPath, utils.CONST_AGENT_ACTION_CLONE, startTime)
					batchSize = 0
					blocks = nil
				}
			} else {
				log.Printf("No more data to read from the source %d", blockCount)
				if len(blocks) > 0 {
					utils.StreamData(blocks, websock, true, srcPath, utils.CONST_AGENT_ACTION_CLONE, startTime)
					log.Printf("Flush remaining data of size %d", batchSize)
				}
				cloneMutex.Lock()
				*isCloning = false
				cloneMutex.Unlock()
				return
			}
			blockCount++
			batchSize++
		}
	}

}
