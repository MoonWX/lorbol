package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/MoonWX/lorbol/internal/config"
)

type Storage interface {
	Store(key string, data interface{}) error
	Retrieve(key string, data interface{}) error
	Delete(key string) error
	List() ([]string, error)
	Close() error
}

type FileStorage struct {
	config   config.StorageConfig
	basePath string
	mu       sync.RWMutex
}

type StorageEntry struct {
	Key       string      `json:"key"`
	Data      interface{} `json:"data"`
	Timestamp time.Time   `json:"timestamp"`
	TTL       time.Duration `json:"ttl,omitempty"`
}

func NewFileStorage(cfg config.StorageConfig) (Storage, error) {
	if err := os.MkdirAll(cfg.Path, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}

	return &FileStorage{
		config:   cfg,
		basePath: cfg.Path,
	}, nil
}

func (fs *FileStorage) Store(key string, data interface{}) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	entry := StorageEntry{
		Key:       key,
		Data:      data,
		Timestamp: time.Now(),
		TTL:       fs.config.Retention,
	}

	jsonData, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal data: %w", err)
	}

	filePath := filepath.Join(fs.basePath, key+".json")
	if err := os.WriteFile(filePath, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

func (fs *FileStorage) Retrieve(key string, data interface{}) error {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	filePath := filepath.Join(fs.basePath, key+".json")
	jsonData, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("key not found: %s", key)
		}
		return fmt.Errorf("failed to read file: %w", err)
	}

	var entry StorageEntry
	if err := json.Unmarshal(jsonData, &entry); err != nil {
		return fmt.Errorf("failed to unmarshal data: %w", err)
	}

	if entry.TTL > 0 && time.Since(entry.Timestamp) > entry.TTL {
		fs.Delete(key)
		return fmt.Errorf("key expired: %s", key)
	}

	entryJSON, err := json.Marshal(entry.Data)
	if err != nil {
		return fmt.Errorf("failed to marshal entry data: %w", err)
	}

	if err := json.Unmarshal(entryJSON, data); err != nil {
		return fmt.Errorf("failed to unmarshal into target: %w", err)
	}

	return nil
}

func (fs *FileStorage) Delete(key string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	filePath := filepath.Join(fs.basePath, key+".json")
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete file: %w", err)
	}

	return nil
}

func (fs *FileStorage) List() ([]string, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	files, err := os.ReadDir(fs.basePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var keys []string
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		
		name := file.Name()
		if filepath.Ext(name) == ".json" {
			key := name[:len(name)-5]
			keys = append(keys, key)
		}
	}

	return keys, nil
}

func (fs *FileStorage) Close() error {
	return fs.cleanup()
}

func (fs *FileStorage) cleanup() error {
	keys, err := fs.List()
	if err != nil {
		return err
	}

	for _, key := range keys {
		var entry StorageEntry
		if err := fs.Retrieve(key, &entry); err != nil {
			if err.Error() == fmt.Sprintf("key expired: %s", key) {
				continue
			}
			return err
		}
	}

	return nil
}