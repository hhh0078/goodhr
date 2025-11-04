# GoodJob AI Assistant

## Project Overview

GoodJob AI Assistant is a browser extension designed to help HR professionals screen and evaluate candidate profiles more efficiently. The extension supports multiple job platforms and offers two modes: Free and AI.

- **Free Mode**: Screens candidates using keyword matching
- **AI Mode**: Uses artificial intelligence for intelligent screening and evaluation

## Core Features

1. **Multi-platform Support**: Compatible with multiple job platforms including BOSS Zhipin, Lagou, Liepin, and Zhaopin
2. **Intelligent Screening**:
   - Keyword matching
   - AI-powered candidate matching assessment
3. **Auto-scrolling**: Automatically scrolls through candidate lists
4. **Candidate Highlighting**: Color-codes candidates based on matching results
5. **Resume Download**: Supports bulk downloading of candidate resumes
6. **Job Description Optimization**: AI-assisted optimization of job descriptions

## Main Components

- **Background Service**: Manages the extension’s core logic and data
- **Content Scripts**: Interact with job platforms to extract and process candidate information
- **Popup Interface**: User configuration panel for setting screening criteria and viewing logs
- **Parsers**: Specialized modules for parsing different job platforms
- **AI Integration**: Integrated with TracerFlow API to enable intelligent screening

## Usage Instructions

1. After installing the extension, click the extension icon on the candidate list page of any supported job platform.
2. In the popup interface, select either “Free Mode” or “AI Mode”.
3. Configure screening criteria:
   - Enter keywords and exclusion terms
   - Set scroll delay and match pause threshold
   - Enable/disable alert sounds
4. Click the “Start” button to begin automated screening.
5. AI Mode users can input job requirements; the system will intelligently assess candidate matching.

## Developer Notes

This project follows a modular design with the following main code structure:

- `content_scripts/`: Browser content scripts responsible for web interaction
- `content_scripts/sites/`: Platform-specific implementations for different job sites
- `popup/`: Popup interface and associated logic
- `background.js`: Background service logic
- `config.js`: Configuration management

## Notes

1. Using AI features incurs API call charges; please use responsibly.
2. A stable internet connection is required to access AI services.
3. Page structures on different job platforms may change over time; parsers may require periodic updates.

## Version Updates

This version includes the following improvements:
- Added AI-powered intelligent screening
- Enhanced multi-platform compatibility
- Improved accuracy of candidate information extraction
- Refined user interface and interaction experience

For the latest version details, please refer to the project repository.