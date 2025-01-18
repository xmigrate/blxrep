package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/gob"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/gorilla/websocket"
	"github.com/xmigrate/blxrep/utils"
	"golang.org/x/sys/unix"
)

func setTargetDiskMajorMinor(objs *utils.BpfObjects, major uint32, minor uint32) error {
	majorKey := uint32(0)
	minorKey := uint32(1)

	// Put major number at index 0
	if err := objs.TargetDiskMap.Put(majorKey, major); err != nil {
		return err
	}

	// Put minor number at index 1
	if err := objs.TargetDiskMap.Put(minorKey, minor); err != nil {
		return err
	}

	return nil
}

func GetBlocks(ctx context.Context, blockSize int, srcPath string, websock *websocket.Conn, agentId string) {
	utils.LogDebug(fmt.Sprintf("Block size: %d", blockSize))
	// Subscribe to signals for terminating the program.
	stopper := make(chan os.Signal, 1)
	signal.Notify(stopper, os.Interrupt, syscall.SIGTERM)

	// Allow the current process to lock memory for eBPF resources.
	if err := rlimit.RemoveMemlock(); err != nil {
		utils.LogError(fmt.Sprintf("Error removing memlock: %v", err))
	}

	fileInfo, err := os.Stat(srcPath)
	if err != nil {
		utils.LogError(fmt.Sprintf("Error retrieving information for /dev/xvda: %v", err))
	}

	// Asserting type to sys stat to get Sys() method
	sysInfo, ok := fileInfo.Sys().(*syscall.Stat_t)
	if !ok {
		utils.LogError("Error asserting type to syscall.Stat_t")
	}

	// Extracting major and minor numbers
	desiredMajor := uint32(sysInfo.Rdev / 256)
	desiredMinor := uint32(sysInfo.Rdev % 256)
	utils.LogDebug(fmt.Sprintf("Major/minor: %d %d", desiredMajor, desiredMinor))

	// Load pre-compiled programs and maps into the kernel.
	objs := utils.BpfObjects{}
	if err := utils.LoadBpfObjects(&objs, nil); err != nil {
		utils.LogError(fmt.Sprintf("loading objects: %v", err))
	}
	defer objs.Close()

	if err := setTargetDiskMajorMinor(&objs, desiredMajor, desiredMinor); err != nil {
		utils.LogError(fmt.Sprintf("setting major/minor: %v", err))
	}
	// create a Tracepoint link
	tp, err := link.Tracepoint("block", "block_rq_complete", objs.BlockRqComplete, nil)
	if err != nil {
		utils.LogError(fmt.Sprintf("opening tracepoint: %s", err))
	}
	defer tp.Close()

	// Open a ringbuf reader from userspace RINGBUF map described in the
	// eBPF C program.
	rd, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		utils.LogError(fmt.Sprintf("opening ringbuf reader: %s", err))
	}
	defer rd.Close()

	// Close the reader when the process receives a signal, which will exit
	// the read loop.
	go func() {
		for {
			select {
			case <-stopper:
				if err := rd.Close(); err != nil {
					utils.LogError(fmt.Sprintf("closing ringbuf reader: %s", err))
				}
				return

			case <-ctx.Done():

				if err := rd.Close(); err != nil {
					utils.LogError(fmt.Sprintf("closing ringbuf reader: %s", err))
				}

				return

			}
		}
	}()

	utils.LogDebug("Waiting for events..")
	for {
		record, err := rd.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				utils.LogDebug("Received signal, exiting..")
				utils.LogDebug(err.Error())
				os.Exit(0)

			}
			utils.LogError(fmt.Sprintf("reading from reader: %s", err))
			continue
		}

		var event utils.Event
		// Parse the ringbuf event entry into a bpfEvent structure.
		if err := binary.Read(bytes.NewBuffer(record.RawSample), binary.LittleEndian, &event); err != nil {
			utils.LogError(fmt.Sprintf("parsing ringbuf event: %s", err))
			continue
		}
		var binaryBuffer bytes.Buffer
		var liveSectors utils.AgentBulkMessage
		liveSectors.AgentID = agentId
		liveSectors.SrcPath = srcPath
		liveSectors.DataType = "cst"
		liveSectors.StartSector = event.Block
		liveSectors.EndSector = event.EndBlock
		enc := gob.NewEncoder(&binaryBuffer)
		if err := enc.Encode(liveSectors); err != nil {
			utils.LogError(fmt.Sprintf("Could not encode: %v", err))
			continue
		}
		binaryData := binaryBuffer.Bytes()

		if err := websock.WriteMessage(websocket.BinaryMessage, binaryData); err != nil {
			utils.LogError(fmt.Sprintf("Could not send blocks: %v", err))
			continue
		}
		utils.LogDebug(fmt.Sprintf("Sent sectors: %d-%d", event.Block, event.EndBlock))
	}
}

func syncAndClearCache(srcPath string) error {
	// For future, if filesystem changes are larger then we only have to use sync, if changes are smaller then we have to do all this
	cmd := exec.Command("sudo", "sync")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to sync: %w", err)
	}

	cmd = exec.Command("sudo", "sh", "-c", "echo 3 > /proc/sys/vm/drop_caches")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to clear cache: %w", err)
	}

	// Open device with read-write permissions
	file, err := os.OpenFile(srcPath, os.O_RDWR, 0666)
	if err != nil {
		return fmt.Errorf("failed to open device: %w", err)
	}
	defer file.Close()

	// File-specific sync
	if err := file.Sync(); err != nil {
		return fmt.Errorf("fsync failed: %w", err)
	}

	// Clear the buffer cache
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, file.Fd(), unix.BLKFLSBUF, uintptr(unsafe.Pointer(nil)))
	if errno != 0 {
		return errno
	}

	return nil
}

func ReadDataBlocks(blockSize int, srcPath string, pair utils.BlockPair, websock *websocket.Conn) {
	utils.LogDebug(fmt.Sprintf("ReadDataBlocks started for blocks %d to %d on %s", pair.Start, pair.End, srcPath))

	var agentBlocks utils.AgentBulkMessage
	agentBlocks.AgentID, _ = os.Hostname()
	agentBlocks.DataType = "incremental"
	agentBlocks.SrcPath = srcPath

	src, err := os.OpenFile(srcPath, os.O_RDONLY|unix.O_DIRECT, 0)
	if err != nil {
		utils.LogError(fmt.Sprintf("Error opening source: %v", err))
		return
	}
	defer src.Close()

	_, err = src.Seek(int64(pair.Start)*int64(blockSize), io.SeekStart)
	if err != nil {
		utils.LogError(fmt.Sprintf("Error seeking to block %d: %v", pair.Start, err))
		return
	}

	for blockNumber := pair.Start; blockNumber <= pair.End; blockNumber++ {
		buf := make([]byte, blockSize)
		n, err := src.Read(buf)
		if err != nil && err != io.EOF {
			utils.LogError(fmt.Sprintf("Error reading block %d: %v", blockNumber, err))
			continue
		}

		block := utils.AgentDataBlock{
			BlockNumber: blockNumber,
			BlockData:   buf[:n],
		}
		checksumArray := sha256.Sum256(block.BlockData)
		block.Checksum = hex.EncodeToString(checksumArray[:])
		agentBlocks.Data = append(agentBlocks.Data, block)

		// Send data if we've accumulated enough or if this is the last block, 4096 should be changed to bandwidth limit
		if len(agentBlocks.Data) >= 4096 || blockNumber == pair.End {
			// Serialize and send data
			var binaryBuffer bytes.Buffer
			enc := gob.NewEncoder(&binaryBuffer)
			if err := enc.Encode(agentBlocks); err != nil {
				utils.LogError(fmt.Sprintf("Could not encode: %v", err))
				continue
			}
			binaryData := binaryBuffer.Bytes()

			if err := websock.WriteMessage(websocket.BinaryMessage, binaryData); err != nil {
				utils.LogError(fmt.Sprintf("Could not send blocks data: %v", err))
				continue
			}

			utils.LogDebug(fmt.Sprintf("Sent batch of %d blocks", len(agentBlocks.Data)))
			agentBlocks.Data = []utils.AgentDataBlock{} // Clear the data for the next batch
		}

		// Allow other goroutines to run
		runtime.Gosched()
	}
}

func ReadBlocks(ctx context.Context, blockSize int, blockPairs []utils.BlockPair, srcPath string, websock *websocket.Conn) {
	utils.LogDebug(fmt.Sprintf("Reading and sending block pairs for %s", srcPath))
	if err := syncAndClearCache(srcPath); err != nil {
		utils.LogError(fmt.Sprintf("Failed to sync and clear cache: %v", err))
	}
	for _, pair := range blockPairs {
		select {
		case <-ctx.Done():
			utils.LogDebug("Context cancelled, stopping ReadBlocks")
			return
		default:
			ReadDataBlocks(blockSize, srcPath, pair, websock)
		}
	}
	utils.LogDebug("Finished reading and sending block pairs")
}

func processSyncAction(msg utils.Message, ws *websocket.Conn) {
	syncData := msg.SyncMessage
	utils.LogDebug(fmt.Sprintf("Start syncing from: %s", syncData.SrcPath))
	ctx, _ := context.WithCancel(context.Background())
	go ReadBlocks(ctx, syncData.BlockSize, syncData.Blocks, syncData.SrcPath, ws)
}

func processStopSyncAction() {
	utils.LogDebug("Stoping sync action")
	if cancelSync != nil {
		utils.LogDebug("Stopping live migrations")
		cancelSync()
	}
	syncMutex.Lock()
	isSyncing = false
	syncMutex.Unlock()
}
