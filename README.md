# TinyAgent
Example LLM agent for academic purposes. It has a very simple architecture:

- The LLM is given a mission and some tools
- It is called repeatedly until it emits a final message

This LLM is **Read-Only** and attempts to stay inside the **Working Directory** as it lacks safety features for writing.

* Note is it possible the Agent can break out of the working directory and send ANY file on your computer to the API.

It supports LM Studio (*recommended*) or any other OpenAI compatible API.

## Usage

```bash
go run github.com/dans-stuff/tinyagent@main
```

## Example

<img width="815" alt="Screenshot 2025-05-17 at 11 50 03 AM" src="https://github.com/user-attachments/assets/2c57ac33-b38a-4f7f-8dfc-192d7982bfcc" />

## Contributing
Contributions are welcome. Please fork the repository and submit pull requests for improvements or bug fixes.

## License
This project is open source and available under the MIT License.
