package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os/exec"
	"sync"

	"github.com/kr/pty"
	"golang.org/x/crypto/ssh"
)

func main() {
	serverConfig := &ssh.ServerConfig{
		NoClientAuth: true,
	}

	privateKeyBytes, err := ioutil.ReadFile("id_rsa")
	if err != nil {
		log.Fatal(err)
	}

	// パスフレーズのある鍵を利用するパターンでは下記で行けると持ったけど無理だった。別途調査が必要
	//passPhrase := []byte("test")
	//privateKey, err := ssh.ParsePrivateKeyWithPassphrase(privateKeyBytes, passPhrase)

	privateKey, err := ssh.ParsePrivateKey(privateKeyBytes)
	if err != nil {
		log.Fatal("Failed to parse private key:", err)
	}

	serverConfig.AddHostKey(privateKey)
	listener, err := net.Listen("tcp", "0.0.0.0:2222")
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Listening on 2222...")

	for {

		// 先にTCPコネクションができていないと接続できない理由はRFCに書いて有りそう。
		tcpConn, err := listener.Accept()
		if err != nil {
			log.Fatalf("Failed to accept on 2222 (%s)", err)
		}

		// RFCに基づいて接続確立方法が正しいのか確認しておく。
		sshConn, chans, reqs, err := ssh.NewServerConn(tcpConn, serverConfig)

		// sshConnで接続元IPアドレスが特定できるのでアクセスもとをtomlファイルでユーザごとに制御することは可能
		// 接続先はsshConnでは特定することができないので、事前にユーザに紐づけをしておくとかにする以外は方法がなさそう。
		// ポートフォワーディングみたいなことをすれば、いいのかな。（鍵認証が使えなくなる気がする）
		log.Println("User:", sshConn.User())
		log.Println("Remote address:", sshConn.RemoteAddr())
		log.Println("Local address:", sshConn.LocalAddr())

		// User Check
		// tomlファイルからアクセスリストを取り出す方式にすれば問題なさそう。
		if sshConn.User() == "mockoro" {
			log.Println("User check OK")
			if err != nil {
				log.Fatalf("Failed to handshake (%s)", err)
			}
			log.Printf("New SSH connection from %s (%s)", sshConn.RemoteAddr(), sshConn.ClientVersion())

			go ssh.DiscardRequests(reqs)
			go handleChannels(chans)
		} else {
			log.Println("User check NG")
			err := sshConn.Close()
			if err != nil {
				log.Println("SSH Connection Close Failed: ", err)
			}
			err = tcpConn.Close()
			if err != nil {
				log.Println("TCP Connection Close Failed: ", err)
			}
		}
	}
}

func handleChannels(chans <-chan ssh.NewChannel) {
	for newChannel := range chans {
		go handleChannel(newChannel)
	}
}

func handleChannel(newChannel ssh.NewChannel) {
	if t := newChannel.ChannelType(); t != "session" {
		newChannel.Reject(ssh.UnknownChannelType, fmt.Sprintf("Unknown channel type: %s", t))
		return
	}

	sshChannel, _, err := newChannel.Accept()
	if err != nil {
		log.Fatalf("Could not accept channel (%s)", err)
		return
	}

	bash := exec.Command("bash")

	close := func() {
		sshChannel.Close()
		_, err := bash.Process.Wait()
		if err != nil {
			log.Printf("Failed to exit bash (%s)", err)
		}
		log.Printf("Session closed")
	}

	f, err := pty.Start(bash)
	if err != nil {
		log.Printf("Could not start pty (%s)", err)
		close()
		return
	}

	var once sync.Once
	go func() {
		io.Copy(sshChannel, f)
		once.Do(close)
	}()
	go func() {
		io.Copy(f, sshChannel)
		once.Do(close)
	}()
}
