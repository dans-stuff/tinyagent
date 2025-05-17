# tinyagent

## Overview
tinyagent is a Go-based tool designed to securely read and analyze text files within the current working directory. It interacts with AI models to process file contents and answer questions about them. The tool includes functionality to validate file paths, detect file types, and efficiently handle large files by reading limited segments.

## Features
- Validate file paths to ensure they are local and within the working directory.
- Detect whether files are text or binary.
- Read and analyze up to 1000 bytes of file content, with random segment selection for large files.
- Interface with AI models to analyze file content and answer user queries.
- Provide tools to browse directories and study file contents programmatically.

## Usage
The tool exposes two main functions:

### browse_directory
Lists immediate children of a target directory relative to the current working directory.

**Parameters:**
- `path` (string): Target directory path, default is `"."`.

### study_file_contents
Analyzes the contents of a specified file and answers a user-provided question.

**Parameters:**
- `path` (string): Target file path, relative to the current working directory.
- `question` (string): The question to ask about the file content.

## Installation
Ensure you have Go installed. Clone the repository and build the project:

```bash
git clone https://github.com/dans-stuff/tinyagent.git
cd tinyagent
go build
```

## Contributing
Contributions are welcome. Please fork the repository and submit pull requests for improvements or bug fixes.

## License
This project is open source and available under the MIT License.