# Saturday - Secure Search Frontend

A Next.js-based frontend application for the Saturday search engine, featuring end-to-end encryption for secure search operations.

## Features

### Security
- **End-to-End Encryption**: All communication with the backend is encrypted using:
  - RSA-OAEP for initial key exchange
  - AES-GCM (256-bit) for subsequent requests/responses
- **Zero Trust Architecture**: Server never sees plaintext search queries

### User Interface
- **Responsive Design**: 5-column masonry layout for search results
- **Real-time Updates**: Dynamic search results with loading states
- **Clean Typography**: Uses Piazzolla font for optimal readability

## Technical Stack

- **Framework**: Next.js 14 with App Router
- **Language**: TypeScript
- **Encryption**: Web Crypto API
  - RSA-OAEP with SHA-256 for key exchange
  - AES-GCM for payload encryption
- **Styling**: CSS Modules

## API Integration

### Encryption Flow
1. Fetches RSA public key from server
2. Generates AES session key
3. Encrypts AES key with RSA
4. Sends encrypted AES key to server
5. Uses AES for all subsequent communications

### Endpoints Used
- `GET /public` - Retrieves RSA public key
- `POST /aes` - Sends encrypted AES key
- `POST /search` - Performs encrypted search

## Getting Started

### Prerequisites
```bash
node >= 18.0.0
npm >= 9.0.0
```

### Installation
```bash
npm install
```

### Development
```bash
npm run dev
```
The application will be available at [http://localhost:3000](http://localhost:3000)

### Production Build
```bash
npm run build
npm run start
```

### Environment Variables
Create a `.env.local` file:
```env
NEXT_PUBLIC_API_BASE=http://your-api-url
```

## Project Structure

```
src/
├── app/
│   ├── page.tsx        # Main search interface
│   ├── page.module.css # Styles for search interface
│   └── layout.tsx      # Root layout
```

## Security Considerations

- All search queries are encrypted before transmission
- AES key is regenerated for each session
- Uses secure random number generation for IVs
- Implements secure key exchange protocol
- No plaintext data is transmitted after initial setup

## Browser Support

Requires browsers with support for:
- Web Crypto API
- ES6+ features
- Fetch API

## License

This project is licensed under the MIT License - see the LICENSE file for details