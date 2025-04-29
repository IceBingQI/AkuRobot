package service

import (
	"aku-web/internal/config"
	"fmt"
	"io"
	"log"
	"os/exec"
	"path/filepath"
	"time"
)

// XiaozhiService 小智AI服务
type XiaozhiService struct {
	*BaseService
	soundCmd *exec.Cmd
	mainCmd  *exec.Cmd
	guiCmd   *exec.Cmd
}

// NewXiaozhiService 创建小智AI服务实例
func NewXiaozhiService() *XiaozhiService {
	return &XiaozhiService{
		BaseService: NewBaseService("xiaozhi"),
	}
}

// Start 启动小智AI服务
func (s *XiaozhiService) Start() error {
	// 1. 调用基础服务的 Start
	if err := s.BaseService.Start(); err != nil {
		return fmt.Errorf("failed to start base service: %v", err)
	}

	// 使用 defer 确保在出错时清理资源
	var started bool
	defer func() {
		if !started {
			s.cleanup()
		}
	}()

	// 2. 启动 sound 服务
	soundCmd := exec.Command(config.XiaozhiSoundPath)
	// 不使用startCommand向channel写入输出，可能会造成僵尸进程，但并不影响服务
	if err := soundCmd.Start(); err != nil {
		return fmt.Errorf("failed to start sound service: %v", err)
	}
	// 添加等待 goroutine
	go func() {
		if err := soundCmd.Wait(); err != nil {
			log.Printf("Sound service exited with error: %v", err)
		}
	}()
	s.soundCmd = soundCmd

	// 3. 启动 gui 服务
	guiCmd := exec.Command(config.XiaozhiGuiPath)
	// 不使用startCommand向channel写入输出，可能会造成僵尸进程，但并不影响服务
	if err := guiCmd.Start(); err != nil {
		return fmt.Errorf("failed to start gui service: %v", err)
	}
	// 添加等待 goroutine
	go func() {
		if err := guiCmd.Wait(); err != nil {
			log.Printf("GUI service exited with error: %v", err)
		}
	}()
	s.guiCmd = guiCmd

	// 4. 启动 main 服务
	mainCmd := exec.Command(config.XiaozhiMainPath)
	if err := s.startCommand(mainCmd); err != nil {
		return fmt.Errorf("failed to start main service: %v", err)
	}
	s.mainCmd = mainCmd

	// 所有服务启动成功
	started = true
	return nil
}

// Stop 停止小智AI服务
func (s *XiaozhiService) Stop() error {
	if err := s.BaseService.Stop(); err != nil {
		return fmt.Errorf("failed to stop base service: %v", err)
	}

	s.cleanup()
	return nil
}

// cleanup 清理所有资源
func (s *XiaozhiService) cleanup() {
	// 获取进程名称
	mainName := filepath.Base(config.XiaozhiMainPath)
	soundName := filepath.Base(config.XiaozhiSoundPath)
	guiName := filepath.Base(config.XiaozhiGuiPath)

	// 停止所有相关进程
	if err := exec.Command("killall", mainName, soundName, guiName).Run(); err != nil {
		s.SendOutput(fmt.Sprintf("Warning: failed to stop services with killall: %v", err))
	}

	// 清理命令对象
	s.soundCmd = nil
	s.mainCmd = nil
	s.guiCmd = nil
}

// startCommand 启动命令并捕获输出
func (s *XiaozhiService) startCommand(cmd *exec.Cmd) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("Failed to get stdout pipe: %v", err)
		return fmt.Errorf("failed to get stdout pipe: %v", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		log.Printf("Failed to get stderr pipe: %v", err)
		return fmt.Errorf("failed to get stderr pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		log.Printf("Failed to start command: %v", err)
		return fmt.Errorf("failed to start command: %v", err)
	}

	// 捕获输出
	go func() {
		log.Printf("Starting output capture goroutine")
		reader := io.MultiReader(stdout, stderr)
		buf := make([]byte, 1024)

		// 创建一个单独的goroutine来等待命令完成
		go func() {
			log.Printf("Starting command wait goroutine")
			if err := cmd.Wait(); err != nil {
				log.Printf("Command exited with error: %v", err)
				// 收到小智主动断开连接信号，停止服务
				s.Stop()
			}
			log.Printf("Command completed")
		}()

		for {
			select {
			case <-s.stopChan:
				log.Printf("Received stop signal in output capture")
				return
			default:
				n, err := reader.Read(buf)
				if n > 0 {
					msg := string(buf[:n])
					log.Printf("Read %d bytes from command output", n)
					s.SendOutput(msg)
				}
				if err != nil {
					if err != io.EOF {
						log.Printf("Read error: %v", err)
						s.SendOutput(fmt.Sprintf("Read error: %v", err))
					}
					time.Sleep(100 * time.Millisecond)
					continue
				}
			}
		}
	}()

	log.Printf("Command started successfully")
	return nil
}
