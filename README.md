# TinyAgent
Example LLM agent for academic purposes. It has a very simple architecture:

- The LLM is given a mission and some tools
- It is called repeatedly until it emits a final message

This LLM is **Read-Only** and limited to the **Working Directory** as it lacks safety features for writing.

It supports LM Studio (*recommended*) or any other OpenAI compatible API.

## Usage

```bash
go run github.com/dans-stuff/tinyagent/...
```

## Example



## Contributing
Contributions are welcome. Please fork the repository and submit pull requests for improvements or bug fixes.

## License
This project is open source and available under the MIT License.