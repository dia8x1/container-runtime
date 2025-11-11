package image

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	RepositoryFile = "/var/lib/container-runtime/repositories.json"
)

func SaveImageMetadata(img *Image) error {
	metadataPath := filepath.Join(ImageDir, fmt.Sprintf("%s.json", img.ID))

	data, err := json.MarshalIndent(img, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal image metadata: %w", err)
	}

	if err := os.WriteFile(metadataPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write metadata file: %w", err)
	}

	return nil
}

func LoadImageMetadata(imageID string) (*Image, error) {
	metadataPath := filepath.Join(ImageDir, fmt.Sprintf("%s.json", imageID))

	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read metadata file: %w", err)
	}

	var img Image
	if err := json.Unmarshal(data, &img); err != nil {
		return nil, fmt.Errorf("failed to unmarshal image metadata: %w", err)
	}

	return &img, nil
}

func UpdateRepository(name, tag, imageID string) error {
	repo, err := LoadRepository()
	if err != nil {
		repo = &Repository{
			Images: make(map[string]string),
		}
	}

	key := fmt.Sprintf("%s:%s", name, tag)
	repo.Images[key] = imageID

	return SaveRepository(repo)
}

func LoadRepository() (*Repository, error) {
	data, err := os.ReadFile(RepositoryFile)
	if err != nil {
		if os.IsNotExist(err) {
			return &Repository{Images: make(map[string]string)}, nil
		}
		return nil, fmt.Errorf("failed to read repository file: %w", err)
	}

	var repo Repository
	if err := json.Unmarshal(data, &repo); err != nil {
		return nil, fmt.Errorf("failed to unmarshal repository: %w", err)
	}

	return &repo, nil
}

func SaveRepository(repo *Repository) error {

	dir := filepath.Dir(RepositoryFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	data, err := json.MarshalIndent(repo, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal repository: %w", err)
	}

	if err := os.WriteFile(RepositoryFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write repository file: %w", err)
	}

	return nil
}

func ResolveImage(nameOrID string) (*Image, error) {
	if strings.HasPrefix(nameOrID, "sha256:") {
		return LoadImageMetadata(nameOrID)
	}

	repo, err := LoadRepository()
	if err != nil {
		return nil, fmt.Errorf("failed to load repository: %w", err)
	}

	if !strings.Contains(nameOrID, ":") {
		nameOrID = nameOrID + ":latest"
	}

	imageID, ok := repo.Images[nameOrID]
	if !ok {
		return nil, fmt.Errorf("image not found: %s", nameOrID)
	}

	return LoadImageMetadata(imageID)
}

func ExtractImage(img *Image, targetDir string) error {
	fmt.Printf("Extracting image %s to %s...\n", img.ID, targetDir)

	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create target directory: %w", err)
	}

	cmd := exec.Command("tar", "-xzf", img.RootfsPath, "-C", targetDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to extract image: %w", err)
	}

	return nil
}

func ListImages() ([]*Image, error) {
	repo, err := LoadRepository()
	if err != nil {
		return nil, fmt.Errorf("failed to load repository: %w", err)
	}

	var images []*Image
	seen := make(map[string]bool)

	for _, imageID := range repo.Images {
		if seen[imageID] {
			continue
		}
		seen[imageID] = true

		img, err := LoadImageMetadata(imageID)
		if err != nil {
			fmt.Printf("Warning: failed to load image %s: %v\n", imageID, err)
			continue
		}

		images = append(images, img)
	}

	return images, nil
}

func GetImageTags(imageID string) []string {
	repo, err := LoadRepository()
	if err != nil {
		return nil
	}

	var tags []string
	for tag, id := range repo.Images {
		if id == imageID {
			tags = append(tags, tag)
		}
	}

	return tags
}
