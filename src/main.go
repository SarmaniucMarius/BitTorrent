package main

import (
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"runtime"
	"slices"
	"strconv"
)

type JobResult struct {
	pieceIndex int
	data       []byte
}

const SHAHashLength = 20
const ClientId = "00112233445566778899"

func main() {
	if len(os.Args) != 3 {
		abort("Executable called incorrectly")
	}

	outputPath := os.Args[1]
	torrentFile := os.Args[2]

	metaInfo := extractMetaInfo(torrentFile)
	fmt.Println("Extracted meta info from: ", torrentFile)

	peersList := requestPeers(metaInfo.announce, metaInfo.infoHash, metaInfo.fileLength)
	fmt.Printf("Recieved %d peers\n", len(peersList))

	totalPieces := len(metaInfo.piecesHash)
	jobQueue := make(chan int, totalPieces)
	finishedJobs := make(chan JobResult)
	for pieceIndex := range metaInfo.piecesHash {
		jobQueue <- pieceIndex
	}

	for _, peerAdr := range peersList {
		go startJob(peerAdr, metaInfo, jobQueue, finishedJobs)
	}

	fileData := make([]byte, metaInfo.fileLength)
	for piecesDone := 0; piecesDone < totalPieces; {
		jobResult := <-finishedJobs
		fileDataBegin := metaInfo.pieceLength * jobResult.pieceIndex
		fileDataEnd := fileDataBegin + len(jobResult.data)
		copy(fileData[fileDataBegin:fileDataEnd], jobResult.data)
		piecesDone++

		numJobs := runtime.NumGoroutine() - 1 // ignore main thread
		progress := (float32(piecesDone) / float32(totalPieces)) * 100.0
		fmt.Printf("Downloaded piece: %d. Total jobs: %d. Progress: %.2f%%\n", jobResult.pieceIndex, numJobs, progress)
	}
	close(jobQueue)

	file, err := os.Create(outputPath)
	if err != nil {
		abort(fmt.Sprintf("Couldn't create file %s. Error: %s\n", outputPath, err.Error()))
	}
	defer file.Close()
	_, err = file.Write(fileData)
	if err != nil {
		abort(fmt.Sprintf("Couldn't write data to file %s. Error: %s\n", file.Name(), err.Error()))
	}

	fmt.Printf("Downloaded %s to %s.\n", torrentFile, outputPath)
}

func startJob(peerAdr string, metaInfo MetaInfo, jobQueue chan int, finishedJobs chan JobResult) {
	peerConn, err := connectToPeer(peerAdr)
	if err != nil {
		fmt.Printf("Couldn't establish tcp connection with %s. Error: %s\n", peerAdr, err.Error())
		return
	}
	defer peerConn.Close()

	_, err = handshakePeer(peerConn, metaInfo.infoHash)
	if err != nil {
		fmt.Printf("Handhake with peer %s failed. Error: %s\n", peerAdr, err.Error())
		return
	}
	fmt.Println("Successful handshake with ", peerAdr)

	peerMsg, err := receiveExpectedMsg(peerConn, Bitfield)
	if err != nil {
		fmt.Printf("Unexpected error while receiving bitfield. Error: %s\n", err.Error())
		return
	}
	bitfieldPayload := peerMsg.payload

	interestedMsg := []byte{0x00, 0x00, 0x00, 0x01, byte(Interested)}
	err = sendData(peerConn, interestedMsg)
	if err != nil {
		fmt.Printf("Unexpected error while sending interested message. Error: %s\n", err.Error())
		return
	}

	_, err = receiveExpectedMsg(peerConn, Unchoke)
	if err != nil {
		fmt.Printf("Unexpected error while receiving unchoke. Error: %s\n", err.Error())
		return
	}

	badHashErrors := 0
	for pieceIndex := range jobQueue {
		if badHashErrors >= 5 {
			fmt.Printf("Peer %s had to many hash errors\n", peerAdr)
			jobQueue <- pieceIndex
			return
		}

		if !hasPiece(pieceIndex, bitfieldPayload) {
			fmt.Printf("Peer doesn't have requested piece with index %d. Returning pieceIndex in jobQueue\n", pieceIndex)
			jobQueue <- pieceIndex
			continue
		}

		pieceData, err := downloadPiece(peerConn, metaInfo, pieceIndex)
		if err != nil {
			fmt.Printf("Unexpected error while downloading piece. Error: %s\n", err.Error())
			jobQueue <- pieceIndex
			return
		}

		expectedHash := []byte(metaInfo.piecesHash[pieceIndex])
		// NOTE: everytime creating a new sh1 object could be too wastefull
		hash := sha1.New()
		io.Writer.Write(hash, pieceData)
		pieceHash := hash.Sum(nil)
		if slices.Compare(expectedHash, pieceHash) != 0 {
			badHashErrors++
			fmt.Printf("Hash for piece index %d doesn't match expected hash. Returning pieceIndex in jobQueue\n", pieceIndex)
			jobQueue <- pieceIndex
			continue
		}

		finishedJobs <- JobResult{
			pieceIndex: pieceIndex,
			data:       pieceData,
		}
	}
}

func requestPeers(trackerUrlStr, infoHash string, fileLength int) []string {
	trackerUrl, err := url.Parse(trackerUrlStr)
	if err != nil {
		abort(fmt.Sprintf("Error parsing tracker url: %s. Error: %s\n", trackerUrl, err.Error()))
	}
	query := trackerUrl.Query()
	query.Set("info_hash", infoHash)
	query.Set("peer_id", ClientId)
	query.Set("port", "6881")
	query.Set("uploaded", "0")
	query.Set("downloaded", "0")
	query.Set("left", strconv.Itoa(fileLength))
	query.Set("compact", "1")
	trackerUrl.RawQuery = query.Encode()

	response, err := http.Get(trackerUrl.String())
	if err != nil {
		abort(fmt.Sprintf("Following request failed: %s. ErrorMsg: %s\n", trackerUrl.String(), err.Error()))
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		abort(fmt.Sprintf("Failed to read response body: %v", err.Error()))
	}

	decoder := Decoder{value: string(body), curIndex: 0}
	decodedValue := decoder.decode()

	trackerResponse, ok := decodedValue.(map[string]interface{})
	if !ok {
		abort("Tracker response should be a bencoded dictionary")
	}

	var peersList []string
	peers := getValue(trackerResponse, "peers", "")
	const PeerLength = 6
	peersCount := len(peers) / PeerLength
	for i := 0; i < peersCount; i++ {
		startIndex := i * PeerLength
		endIndex := startIndex + PeerLength
		peer := peers[startIndex:endIndex]
		peerIp := peer[:5]
		peerPort := binary.BigEndian.Uint16([]byte(peer[4:]))
		peerAddr := fmt.Sprintf("%d.%d.%d.%d:%d", peerIp[0], peerIp[1], peerIp[2], peerIp[3], peerPort)
		peersList = append(peersList, peerAddr)
	}

	return peersList
}

type MetaInfo struct {
	announce    string
	fileLength  int
	pieceLength int
	infoHash    string
	piecesHash  []string
}

func extractMetaInfo(filePath string) MetaInfo {
	data, err := os.ReadFile(filePath)
	if err != nil {
		abort(fmt.Sprintf("Couldn't read file: %s", filePath))
	}

	decoder := Decoder{value: string(data), curIndex: 0}
	decodedValue := decoder.decode()

	metaInfo, ok := decodedValue.(map[string]interface{})
	if !ok {
		abort("Torrent file should be a bencoded dictionary")
	}

	announce := getValue(metaInfo, "announce", "")
	fileInfo := getValue(metaInfo, "info", make(map[string]interface{}, 0))
	fileLength := getValue(fileInfo, "length", 0)
	pieceLength := getValue(fileInfo, "piece length", 0)
	concatenatedPieces := getValue(fileInfo, "pieces", "")

	encodedInfo := encodeDict(fileInfo)
	hash := sha1.New()
	io.WriteString(hash, encodedInfo)

	var pieces []string
	totalPieces := len(concatenatedPieces) / SHAHashLength
	for i := 0; i < totalPieces; i++ {
		hashStart := i * SHAHashLength
		hashEnd := hashStart + SHAHashLength
		pieceHash := concatenatedPieces[hashStart:hashEnd]
		pieces = append(pieces, pieceHash)
	}

	return MetaInfo{
		announce:    announce,
		fileLength:  fileLength,
		pieceLength: pieceLength,
		infoHash:    string(hash.Sum(nil)),
		piecesHash:  pieces,
	}
}

func getValue[T any](metaInfo map[string]interface{}, key string, expectedType T) T {
	value, keyExists := metaInfo[key]
	if !keyExists {
		abort(fmt.Sprintf("Dictionary doesn't contain key: %s", key))
	}
	typedValue, ok := value.(T)
	if !ok {
		abort(fmt.Sprintf("The value: %s is of type %s, but expected type: %s", value, reflect.TypeOf(value), reflect.TypeOf(expectedType)))
	}

	return typedValue
}

func abort(errorMsg string) {
	fmt.Println(errorMsg)
	os.Exit(1)
}
