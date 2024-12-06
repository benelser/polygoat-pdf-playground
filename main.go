package main

import (
	"bytes"
	"compress/zlib"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jung-kurt/gofpdf"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/urfave/cli/v2"
)

// Encryption key (32 bytes for AES-256)
var encryptionKey = []byte("12345678901234567890123456789012")

// encrypt encrypts data using AES in CFB mode.
func encrypt(data []byte) ([]byte, error) {
	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	ciphertext := make([]byte, aes.BlockSize+len(data))
	iv := ciphertext[:aes.BlockSize]
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, fmt.Errorf("failed to generate IV: %w", err)
	}

	stream := cipher.NewCFBEncrypter(block, iv)
	stream.XORKeyStream(ciphertext[aes.BlockSize:], data)

	return ciphertext, nil
}

// decrypt decrypts data encrypted with AES in CFB mode.
func decrypt(data []byte) ([]byte, error) {
	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	if len(data) < aes.BlockSize {
		return nil, errors.New("ciphertext too short")
	}

	iv := data[:aes.BlockSize]
	ciphertext := data[aes.BlockSize:]

	stream := cipher.NewCFBDecrypter(block, iv)
	stream.XORKeyStream(ciphertext, ciphertext)

	return ciphertext, nil
}

// createPDFWithAttachment creates a PDF with an embedded attachment.
func createPDFWithAttachment(pdfFile string, embeddedData []byte) error {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.AddPage()

	pdf.SetFont("Arial", "B", 16)
	pdf.Cell(40, 10, "Reports")
	pdf.Ln(10)
	pdf.SetFont("Arial", "", 12)
	pdf.MultiCell(0, 10, "ACME Corp Monthly Report", "", "L", false)

	attachment := gofpdf.Attachment{
		Content:     embeddedData,
		Filename:    "report.csv",
		Description: "Monthly Report",
	}

	pdf.SetAttachments([]gofpdf.Attachment{attachment})

	if err := pdf.OutputFileAndClose(pdfFile); err != nil {
		return fmt.Errorf("failed to save PDF: %w", err)
	}
	return nil
}

// extractAttachmentFromPDF extracts an embedded file from a PDF.
func extractAttachmentFromPDF(pdfFile string) ([]byte, error) {
	file, err := os.Open(pdfFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open PDF file: %w", err)
	}
	defer file.Close()

	conf := model.NewDefaultConfiguration()
	attachments, err := api.Attachments(file, conf)
	if err != nil {
		return nil, fmt.Errorf("failed to list attachments: %w", err)
	}

	if len(attachments) == 0 {
		return nil, fmt.Errorf("no attachments found in PDF")
	}

	attachment := attachments[0]

	file.Seek(0, io.SeekStart)
	extractedAttachments, err := api.ExtractAttachmentsRaw(file, "", []string{attachment.FileName}, conf)
	if err != nil {
		return nil, fmt.Errorf("failed to extract attachment content: %w", err)
	}

	if len(extractedAttachments) == 0 {
		return nil, fmt.Errorf("no content found for attachment: %s", attachment.FileName)
	}

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, extractedAttachments[0].Reader); err != nil {
		return nil, fmt.Errorf("failed to read extracted attachment content: %w", err)
	}

	return buf.Bytes(), nil
}

// decompress decompresses data using zlib.
func decompress(compressedData []byte) ([]byte, error) {
	reader, err := zlib.NewReader(bytes.NewReader(compressedData))
	if err != nil {
		return nil, fmt.Errorf("failed to create zlib reader: %w", err)
	}
	defer reader.Close()

	var decompressedData bytes.Buffer
	if _, err := io.Copy(&decompressedData, reader); err != nil {
		return nil, fmt.Errorf("failed to read from zlib reader: %w", err)
	}

	return decompressedData.Bytes(), nil
}

// createGitBundle creates a Git bundle from a repository (local or remote).
func createGitBundle(repoPath string) (string, error) {
	isRemote := isRemoteRepo(repoPath)
	var tempDir string

	if isRemote {
		var err error
		tempDir, err = os.MkdirTemp("", "temp-repo-*")
		if err != nil {
			return "", fmt.Errorf("failed to create temporary directory: %w", err)
		}
		defer os.RemoveAll(tempDir)

		cmd := exec.Command("git", "clone", "--bare", repoPath, tempDir)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("failed to clone remote repository: %w", err)
		}
		repoPath = tempDir
	}

	bundleFile := filepath.Join(repoPath, "repo.bundle")
	cmd := exec.Command("git", "-C", repoPath, "bundle", "create", bundleFile, "--all")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to create Git bundle: %w", err)
	}

	finalBundlePath := filepath.Join(".", "repo.bundle")
	if err := os.Rename(bundleFile, finalBundlePath); err != nil {
		return "", fmt.Errorf("failed to move Git bundle file: %w", err)
	}

	return finalBundlePath, nil
}

// isRemoteRepo checks if a repository path is a remote URL.
func isRemoteRepo(repo string) bool {
	return strings.HasPrefix(repo, "http://") || strings.HasPrefix(repo, "https://") || strings.HasPrefix(repo, "git@")
}

// embedRepositoryIntoPDF embeds a Git repository into a PDF.
func embedRepositoryIntoPDF(repoPath, outputFile string) error {
	bundleFile, err := createGitBundle(repoPath)
	if err != nil {
		return fmt.Errorf("failed to create Git bundle: %w", err)
	}
	defer os.Remove(bundleFile)

	bundleData, err := os.ReadFile(bundleFile)
	if err != nil {
		return fmt.Errorf("failed to read Git bundle: %w", err)
	}

	encryptedData, err := encrypt(bundleData)
	if err != nil {
		return fmt.Errorf("failed to encrypt data: %w", err)
	}

	var compressedData bytes.Buffer
	zlibWriter := zlib.NewWriter(&compressedData)
	if _, err := zlibWriter.Write(encryptedData); err != nil {
		return fmt.Errorf("failed to compress encrypted data: %w", err)
	}
	zlibWriter.Close()

	if err := createPDFWithAttachment(outputFile, compressedData.Bytes()); err != nil {
		return fmt.Errorf("failed to create PDF with embedded data: %w", err)
	}

	return nil
}

// extractRepositoryFromPDF extracts and clones a Git repository from a PDF.
func extractRepositoryFromPDF(inputFile, outputDir string) error {
	// Extract the embedded data from the PDF
	embeddedData, err := extractAttachmentFromPDF(inputFile)
	if err != nil {
		return fmt.Errorf("failed to extract attachment from PDF: %w", err)
	}

	// Decompress the embedded data
	decompressedData, err := decompress(embeddedData)
	if err != nil {
		return fmt.Errorf("failed to decompress data: %w", err)
	}

	// Decrypt the decompressed data
	decryptedData, err := decrypt(decompressedData)
	if err != nil {
		return fmt.Errorf("failed to decrypt data: %w", err)
	}

	// Create a temporary file for the Git bundle
	tempFile, err := os.CreateTemp("", "temp-bundle-*.bundle")
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer os.Remove(tempFile.Name()) // Ensure cleanup of the temporary file

	// Write the decrypted data to the temporary file
	if _, err := tempFile.Write(decryptedData); err != nil {
		return fmt.Errorf("failed to write Git bundle to temp file: %w", err)
	}

	// Close the file to flush the data
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// Verify the Git bundle
	cmdVerify := exec.Command("git", "bundle", "verify", tempFile.Name())
	cmdVerify.Stdout = os.Stdout
	cmdVerify.Stderr = os.Stderr
	if err := cmdVerify.Run(); err != nil {
		return fmt.Errorf("invalid Git bundle: %w", err)
	}

	// Clone the repository from the Git bundle
	cmdClone := exec.Command("git", "clone", tempFile.Name(), outputDir)
	cmdClone.Stdout = os.Stdout
	cmdClone.Stderr = os.Stderr
	if err := cmdClone.Run(); err != nil {
		return fmt.Errorf("failed to clone repository: %w", err)
	}

	return nil
}

func main() {
	app := &cli.App{
		Name:  "Git PDF Polyglot Tool",
		Usage: "Embed and extract Git repositories from PDFs using attachments",
		Commands: []*cli.Command{
			{
				Name:  "embed",
				Usage: "Embed a Git repository into a PDF",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "repo", Usage: "Path to the Git repository (local or remote)", Required: true},
					&cli.StringFlag{Name: "output", Usage: "Path to the output PDF file", Required: true},
				},
				Action: func(c *cli.Context) error {
					repoPath := c.String("repo")
					outputFile := c.String("output")
					return embedRepositoryIntoPDF(repoPath, outputFile)
				},
			},
			{
				Name:  "extract",
				Usage: "Extract and clone a Git repository from a PDF",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "input", Usage: "Path to the input PDF file", Required: true},
					&cli.StringFlag{Name: "output", Usage: "Directory to clone the repository", Required: true},
				},
				Action: func(c *cli.Context) error {
					inputFile := c.String("input")
					outputDir := c.String("output")
					return extractRepositoryFromPDF(inputFile, outputDir)
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
