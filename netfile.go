package zlfile

import (
	"fmt"
	"net"
	"os"
	"path"
	"strconv"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type sshFtpService struct {
	Para *Auth
}

func NewSshFtpInterface(para *Auth) SshFtpInterface {
	if para.Port == nil {
		port := 22
		para.Port = &port
	}
	if para.FileBufSize == nil {
		fileBufSize := 10240
		para.FileBufSize = &fileBufSize
	}
	return &sshFtpService{para}
}

func (s *sshFtpService) GetSshSession() (*ssh.Session, error) {
	var (
		auth         []ssh.AuthMethod
		addr         string
		clientConfig *ssh.ClientConfig
		client       *ssh.Client
		session      *ssh.Session
		err          error
	)
	// get auth method
	auth = make([]ssh.AuthMethod, 0)
	auth = append(auth, ssh.Password(s.Para.Password))

	clientConfig = &ssh.ClientConfig{
		User:    s.Para.User,
		Auth:    auth,
		Timeout: 30 * time.Second,
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			return nil
		},
	}

	// connet to ssh
	addr = fmt.Sprintf("%s:%d", s.Para.Host, *s.Para.Port)

	if client, err = ssh.Dial("tcp", addr, clientConfig); err != nil {
		return nil, err
	}

	// create session
	if session, err = client.NewSession(); err != nil {
		return nil, err
	}

	return session, nil
}

// 远程执行cmd命令
func (s *sshFtpService) SshRun(client *ssh.Session, cmd string) error {
	if client == nil {
		var err error
		client, err = s.GetSshSession()
		if err != nil {
			return err
		}
	}
	return client.Run(cmd)
}

func (s *sshFtpService) GetSshClient() (*ssh.Client, error) {
	var (
		auth         []ssh.AuthMethod
		addr         string
		clientConfig *ssh.ClientConfig
	)
	// get auth method
	auth = make([]ssh.AuthMethod, 0)
	auth = append(auth, ssh.Password(s.Para.Password))
	clientConfig = &ssh.ClientConfig{
		User:    s.Para.User,
		Auth:    auth,
		Timeout: 30 * time.Second,
		//这个是问你要不要验证远程主机，以保证安全性。这里不验证
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			return nil
		},
	}
	// connet to ssh
	addr = fmt.Sprintf("%s:%d", s.Para.Host, *s.Para.Port)
	return ssh.Dial("tcp", addr, clientConfig)
}

func (s *sshFtpService) GetFtpClient(client *ssh.Client) (*sftp.Client, error) {
	if client == nil {
		var err error
		client, err = s.GetSshClient()
		if err != nil {
			return nil, err
		}
	}
	return sftp.NewClient(client)
}

// 将from(可以是目录或文件)远程拷贝到to目录下
func (s *sshFtpService) ScpCopyTo(client *sftp.Client, from, to string, chmodFileList []*ChmodFile, ignoresList []string) error {
	if client == nil {
		var err error
		client, err = s.GetFtpClient(nil)
		if err != nil {
			return err
		}
	}
	for _, i := range ignoresList {
		if i == from {
			return nil
		}
	}
	isDir, err := IsDir(from)
	if err != nil {
		return err
	}
	if isDir {
		fileList, err := GetDirFileList(from)
		if err != nil {
			return err
		}
		for _, fileName := range fileList {
			newFrom := path.Join(from, fileName)
			newTo := path.Join(to, fileName)
			isIgnore := false
			for _, i := range ignoresList {
				if i == newFrom {
					isIgnore = true
					break
				}
			}
			if isIgnore {
				continue
			}
			isDir, err := IsDir(newFrom)
			if err != nil {
				return err
			}
			if isDir {
				if err := client.MkdirAll(newTo); err != nil {
					return err
				}
				if err := s.ScpCopyTo(client, newFrom, newTo, chmodFileList, ignoresList); err != nil {
					return err
				}
			} else {
				if err = s.ScpCopyFileTo(client, newFrom, newTo, chmodFileList); err != nil {
					return err
				}
			}
		}
	} else {
		newTo := path.Join(to, path.Base(from))
		return s.ScpCopyFileTo(client, from, newTo, chmodFileList)
	}
	return nil
}

// 将from文件远程拷贝到to目录下
func (s *sshFtpService) ScpCopyFileTo(client *sftp.Client, from, to string, chmodFileList []*ChmodFile) error {
	if client == nil {
		var err error
		client, err = s.GetFtpClient(nil)
		if err != nil {
			return err
		}
	}

	dirPath := GetParentDirectory(to)
	if isExist, _ := s.PathExists(client, dirPath); !isExist {
		if err := client.MkdirAll(dirPath); err != nil {
			return err
		}
	}

	srcFile, err := os.Open(from)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := client.Create(to)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	buf := make([]byte, *s.Para.FileBufSize)
	for {
		n, _ := srcFile.Read(buf)
		if n == 0 {
			break
		}
		dstFile.Write(buf[0:n])
	}

	for _, item := range chmodFileList {
		if to == item.Path {
			return s.Chmod(client, to, item)
			break
		}
	}

	return nil
}

// 将from(可以是目录或文件)远程拷贝到to目录下
func (s *sshFtpService) ScpCopyFrom(client *sftp.Client, from, to string, chmodFileList []*ChmodFile, ignoresList []string) error {
	if client == nil {
		var err error
		client, err = s.GetFtpClient(nil)
		if err != nil {
			return err
		}
	}

	for _, i := range ignoresList {
		if i == from {
			return nil
		}
	}

	isDir, err := s.IsDir(client, from)
	if err != nil {
		return err
	}
	if isDir {
		fileList, err := s.GetDirFileList(client, from)
		if err != nil {
			return err
		}
		for _, fileName := range fileList {
			newFrom := path.Join(from, fileName)
			newTo := path.Join(to, fileName)
			isIgnore := false
			for _, i := range ignoresList {
				if i == newFrom {
					isIgnore = true
					break
				}
			}
			if isIgnore {
				continue
			}
			isDir, err := s.IsDir(client, newFrom)
			if err != nil {
				return err
			}
			if isDir {
				if err := os.MkdirAll(newTo, DIRPERMISSION); err != nil {
					return err
				}
				if err := s.ScpCopyFrom(client, newFrom, newTo, chmodFileList, ignoresList); err != nil {
					return err
				}
			} else {
				if err = s.ScpCopyFileFrom(client, newFrom, newTo, chmodFileList); err != nil {
					return err
				}
			}
		}
	} else {
		newTo := path.Join(to, path.Base(from))
		return s.ScpCopyFileFrom(client, from, newTo, chmodFileList)
	}
	return nil
}

// 将from文件远程拷贝到to目录下
func (s *sshFtpService) ScpCopyFileFrom(client *sftp.Client, from, to string, chmodFileList []*ChmodFile) error {
	if client == nil {
		var err error
		client, err = s.GetFtpClient(nil)
		if err != nil {
			return err
		}
	}

	dirPath := GetParentDirectory(to)
	if isExist, _ := PathExists(dirPath); !isExist {
		if err := os.MkdirAll(dirPath, DIRPERMISSION); err != nil {
			return err
		}
	}

	srcFile, err := client.Open(from)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(to)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	buf := make([]byte, *s.Para.FileBufSize)
	for {
		n, _ := srcFile.Read(buf)
		if n == 0 {
			break
		}
		dstFile.Write(buf[0:n])
	}

	for _, item := range chmodFileList {
		if to == item.Path {
			return Chmod(to, item)
			break
		}
	}

	return nil
}

// 将path文件权限为filePermission
func (s *sshFtpService) Chmod(client *sftp.Client, path string, modeFile *ChmodFile) error {
	if client == nil {
		var err error
		client, err = s.GetFtpClient(nil)
		if err != nil {
			return err
		}
	}
	//cmd := fmt.Sprintf("chmod %s %s", filePermission, path)
	//return s.SshRun(nil, cmd)
	if modeFile.Mode == 0 {
		if len(modeFile.FilePermission) > 0 {
			if modeFile.FilePermission[0] != '0' {
				modeFile.FilePermission = "0" + modeFile.FilePermission
			}
			permNum, err := strconv.ParseInt(modeFile.FilePermission, 8, 10)
			if err != nil {
				return err
			}
			modeFile.Mode = os.FileMode(permNum)
		}
	}
	return client.Chmod(path, modeFile.Mode)
}

// 判断远程路径是否为目录
func (s *sshFtpService) IsDir(client *sftp.Client, path string) (bool, error) {
	if client == nil {
		var err error
		client, err = s.GetFtpClient(nil)
		if err != nil {
			return false, err
		}
	}
	f, err := client.Stat(path)
	if err != nil {
		return false, err
	}
	return f.IsDir(), nil
}

// 获取路径下文件及文件夹名称列表
func (s *sshFtpService) GetDirFileList(client *sftp.Client, path string) ([]string, error) {
	var fileList []string
	if client == nil {
		var err error
		client, err = s.GetFtpClient(nil)
		if err != nil {
			return fileList, err
		}
	}
	rd, err := client.ReadDir(path)
	if err != nil {
		return fileList, err
	}
	for _, fi := range rd {
		fileList = append(fileList, fi.Name())
	}
	return fileList, nil
}

// 判断文件或目录是否存在
func (s *sshFtpService) PathExists(client *sftp.Client, path string) (bool, error) {
	_, err := client.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
