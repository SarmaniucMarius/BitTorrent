package main

import (
	"net"
)

func connectToPeer(peerAddr string) (*net.TCPConn, error) {
	tcpAddr, err := net.ResolveTCPAddr("tcp", peerAddr)
	if err != nil {
		return nil, err
	}
	peerConn, err := net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		return nil, err
	}

	return peerConn, nil
}

func sendData(peerConn *net.TCPConn, data []byte) error {
	for totalBytesWritten := 0; totalBytesWritten < len(data); {
		bytesWritten, err := peerConn.Write(data[totalBytesWritten:])
		if err != nil {
			return err
		}
		totalBytesWritten += bytesWritten
	}
	return nil
}

// TODO: Try to rewrite the function to use only one slice rather than two
func receiveData(peerConn *net.TCPConn, bufferSize int) ([]byte, error) {
	response := make([]byte, bufferSize)
	spaceLeft := bufferSize
	for totalBytesRead := 0; totalBytesRead < bufferSize; {
		buf := make([]byte, spaceLeft)
		bytesRead, err := peerConn.Read(buf)
		if err != nil {
			return nil, err
		}

		copy(response[totalBytesRead:totalBytesRead+bytesRead], buf)
		totalBytesRead += bytesRead
		spaceLeft -= bytesRead
	}

	return response, nil
}
