package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"net"
	"slices"
)

type PeerMessageId int

const (
	Choke PeerMessageId = iota
	Unchoke
	Interested
	NotInterested
	Have
	Bitfield
	Request
	Piece
	Cancel
)

type PeerMessage struct {
	length    int
	messageId PeerMessageId
	payload   []byte
}

func receiveExpectedMsg(peerConn *net.TCPConn, expected PeerMessageId) (PeerMessage, error) {
	peerMsg, err := receivePeerMsg(peerConn)
	if peerMsg.messageId != expected {
		return PeerMessage{}, fmt.Errorf("expected message of type %d received :%d", expected, peerMsg.messageId)
	}
	return peerMsg, err
}

// All messages have the form <length prefix><message ID><payload>.
// <length prefix> - is 4 bytes
// <message ID> - 1 byte
// <payload> - varies, <length prefix> contains the size of the payload
func receivePeerMsg(peerConn *net.TCPConn) (PeerMessage, error) {
GET_MESSAGE_LENGTH:
	// First read the length of the message. This way we will know how big of a slice to create for the <payload> + <message ID>
	lengthPrefix, err := receiveData(peerConn, 4)
	if err != nil {
		return PeerMessage{}, err
	}
	msgLength := binary.BigEndian.Uint32(lengthPrefix)

	// If <length prefix> is 0 this is a keep alive package. This messages should be ignored
	if msgLength == 0 {
		goto GET_MESSAGE_LENGTH
	}

	peerMsg, err := receiveData(peerConn, int(msgLength))
	if err != nil {
		return PeerMessage{}, err
	}

	result := PeerMessage{
		length:    int(msgLength),
		messageId: PeerMessageId(peerMsg[0]),
	}
	// Messages with length 1 have no <payload>, only <message ID>
	if msgLength == 1 {
		return result, nil
	} else {
		result.payload = peerMsg[1:]
		return result, nil
	}
}

func handshakePeer(peerConn *net.TCPConn, infoHash string) (string, error) {
	const ProtocolSignature = "BitTorrent protocol"
	var payload []byte
	payload = append(payload, byte(len(ProtocolSignature)))
	payload = append(payload, []byte(ProtocolSignature)...)
	payload = append(payload, make([]byte, 8)...)
	payload = append(payload, []byte(infoHash)...)
	payload = append(payload, []byte(ClientId)...)

	err := sendData(peerConn, payload)
	if err != nil {
		return "", err
	}

	reply, err := receiveData(peerConn, len(payload))
	if err != nil {
		return "", err
	}
	reservedBytesIndex := 1 + len(ProtocolSignature)
	peerIdIndex := len(payload) - len(ClientId)
	if slices.Compare(payload[:reservedBytesIndex], reply[:reservedBytesIndex]) != 0 {
		return "", fmt.Errorf("peer response is not the same format")
	}

	peerId := string(reply[peerIdIndex:])
	return peerId, nil
}

func downloadPiece(peerConn *net.TCPConn, metaInfo MetaInfo, pieceIndex int) ([]byte, error) {
	totalPieces := len(metaInfo.piecesHash)
	pieceLength := metaInfo.pieceLength
	if pieceIndex == totalPieces-1 && (metaInfo.fileLength%metaInfo.pieceLength) != 0 {
		pieceLength = metaInfo.fileLength % metaInfo.pieceLength
	}

	var pieceData []byte
	blockSize := 16 * 1024
	totalBlocks := int(math.Ceil(float64(pieceLength) / float64(blockSize)))
	totalBytesRead := 0
	// fmt.Printf("Total size of a pice is %d. Requesting %d blocks\n", pieceLength, totalBlocks)
	for i := 0; i < totalBlocks; {
		payloadSize := 1 + 4 + 4 + 4
		var requestMsg []byte
		requestMsg = binary.BigEndian.AppendUint32(requestMsg, uint32(payloadSize))
		requestMsg = append(requestMsg, byte(Request))
		requestMsg = binary.BigEndian.AppendUint32(requestMsg, uint32(pieceIndex)) // piece index
		pieceOffset := i * blockSize
		requestMsg = binary.BigEndian.AppendUint32(requestMsg, uint32(pieceOffset)) // begin
		blockLength := 0
		if i == totalBlocks-1 {
			blockLength = pieceLength - totalBytesRead
		} else {
			blockLength = blockSize
		}
		requestMsg = binary.BigEndian.AppendUint32(requestMsg, uint32(blockLength)) // block length
		err := sendData(peerConn, requestMsg)
		if err != nil {
			return nil, err
		}
		// fmt.Printf("%d. Sent request message for piece with index %d. Begin: %d. Block length: %d\n", i, pieceIndex, pieceOffset, blockLength)

		peerMsg, err := receivePeerMsg(peerConn)
		if err != nil {
			return nil, err
		}
		if peerMsg.messageId == Piece {
			blockData := peerMsg.payload[8:]
			blockSize := len(blockData)

			totalBytesRead += blockSize
			pieceData = append(pieceData, blockData...)
			i++
			// responsePieceIndex := binary.BigEndian.Uint32(peerMsg.payload[:4])
			// responsePieceOffset := binary.BigEndian.Uint32(peerMsg.payload[4:8])
			// fmt.Printf("%d. Received piece message with index %d. Begin: %d. Block length: %d\n", i, responsePieceIndex, responsePieceOffset, blockSize)
		} else if peerMsg.messageId == Choke {
			receiveExpectedMsg(peerConn, Unchoke)
		} else if peerMsg.messageId == Unchoke {
			// ignore it
		} else {
			abort(fmt.Sprintf("Expected to receive unchoke, received %d\n", peerMsg.messageId))
		}
	}

	return pieceData, nil
}

func hasPiece(pieceIndex int, payload []byte) bool {
	byteIndex := pieceIndex / 8
	offset := 7 - (pieceIndex % 8)
	if byteIndex >= len(payload) {
		return false
	}
	targetByte := payload[byteIndex]
	return ((targetByte >> offset) & 0b1) == 1
}
