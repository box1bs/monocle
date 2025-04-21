'use client';

import React, { useEffect, useState } from 'react';
import Head from 'next/head';
import { Piazzolla } from 'next/font/google';
import styles from './page.module.css';

// Утилиты для шифрования
async function importRSAPublicKey(pem: string): Promise<CryptoKey> {
    const base64 = pem
        .replace(/-----BEGIN (?:RSA )?PUBLIC KEY-----/, '')
        .replace(/-----END (?:RSA )?PUBLIC KEY-----/, '')
        .replace(/\s/g, '');
    const der = Uint8Array.from(atob(base64), (c) => c.charCodeAt(0));
    return crypto.subtle.importKey(
        'spki',
        der.buffer,
        { name: 'RSA-OAEP', hash: 'SHA-256' },
        true,
        ['encrypt']
    );
}

async function generateAESKey(): Promise<CryptoKey> {
    return crypto.subtle.generateKey(
        { name: 'AES-GCM', length: 256 },
        true,
        ['encrypt', 'decrypt']
    );
}

async function exportAESKeyRaw(key: CryptoKey): Promise<Uint8Array> {
    const raw = await crypto.subtle.exportKey('raw', key);
    return new Uint8Array(raw);
}

async function rsaEncrypt(publicKey: CryptoKey, data: Uint8Array): Promise<Uint8Array> {
    const buffer = await crypto.subtle.encrypt({ name: 'RSA-OAEP' }, publicKey, data);
    return new Uint8Array(buffer);
}

async function aesEncryptRaw(key: CryptoKey, plaintext: string): Promise<Uint8Array> {
    const iv = crypto.getRandomValues(new Uint8Array(12));
    const encoded = new TextEncoder().encode(plaintext);
    const cipherBuffer = await crypto.subtle.encrypt(
        { name: 'AES-GCM', iv },
        key,
        encoded
    );
    const cipherBytes = new Uint8Array(cipherBuffer);
    const combined = new Uint8Array(iv.length + cipherBytes.length);
    combined.set(iv, 0);
    combined.set(cipherBytes, iv.length);
    return combined;
}

async function aesDecryptRaw(key: CryptoKey, combined: Uint8Array): Promise<string> {
    const iv = combined.slice(0, 12);
    const ciphertext = combined.slice(12);
    const plainBuffer = await crypto.subtle.decrypt(
        { name: 'AES-GCM', iv },
        key,
        ciphertext
    );
    return new TextDecoder().decode(plainBuffer);
}

const API_BASE = process.env.NEXT_PUBLIC_API_BASE || "http://0.0.0.0:50051";

interface SearchResult {
    url: string;
    description: string;
}

const piazzolla = Piazzolla({
    subsets: ['latin', 'cyrillic'],
    weight: ['400', '700'],
    variable: '--font-piazzolla',
});

export default function SearchPage() {
    const [aesKey, setAesKey] = useState<CryptoKey | null>(null);
    const [query, setQuery] = useState('');
    const [results, setResults] = useState<SearchResult[]>([]);
    const [loading, setLoading] = useState(false);

    // Truncate descriptions to 80 characters
    const truncate = (s: string, max = 80) =>
        s.length > max ? s.slice(0, max - 1) + '…' : s;

    useEffect(() => {
        (async () => {
            try {
                const pem = await fetch(`${API_BASE}/public`).then((r) => r.text());
                const rsaKey = await importRSAPublicKey(pem);
                const aes = await generateAESKey();
                setAesKey(aes);
                const rawKey = await exportAESKeyRaw(aes);
                const encryptedKey = await rsaEncrypt(rsaKey, rawKey);
                const b64 = btoa(String.fromCharCode(...encryptedKey));
                await fetch(`${API_BASE}/aes`, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(b64),
                });
            } catch (e) {
                console.error('encryption error:', e);
            }
        })();
    }, []);

    const handleSearch = async () => {
        if (!aesKey) return;
        setLoading(true);
        try {
            const payload = { job_id: '', query, max_results: 50 };
            const plain = JSON.stringify(payload);
            const cipherBytes = await aesEncryptRaw(aesKey, plain);

            const { data: b64 } = await fetch(`${API_BASE}/search`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(Array.from(cipherBytes)),
            }).then((r) => r.json());
            const cipherResp = Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));

            const jsonStr = await aesDecryptRaw(aesKey, cipherResp);
            const data = JSON.parse(jsonStr) as SearchResult[];
            setResults(data);
        } catch (e) {
            console.error('Ошибка поиска:', e);
        } finally {
            setLoading(false);
        }
    };

    const columns = Array.from({ length: 5 }, (_, colIndex) =>
        results.slice(colIndex * 10, colIndex * 10 + 10)
    );

    return (
        <>
            <Head>
                <title>Saturday</title>
                <meta name="description" content="secure search without " />
            </Head>
            <div className={`${piazzolla.variable} ${styles.searchPage}`}>
                {/* Fixed header */}
                <header className={styles.topBar}>
                    <span className={styles.logoText}>Saturday</span>
                </header>

                {/* Search bar */}
                <div className={styles.searchBar}>
                    <input
                        type="text"
                        value={query}
                        onChange={(e) => setQuery(e.target.value)}
                        placeholder="enter query"
                    />
                    <button onClick={handleSearch} disabled={loading || !aesKey}>
                        {loading ? 'Searching...' : 'Search'}
                    </button>
                </div>

                {/* 5-column layout */}
                <div className={styles.grid}>
                    {columns.map((col, ci) => (
                        <div key={ci} className={styles.column}>
                            {col.map((r, i) => (
                                <div
                                    key={i}
                                    className={styles.cell}>
                                    <a
                                        href={r.url}
                                        target="_blank"
                                        rel="noopener noreferrer"
                                        style={{ fontWeight: 'bold' }}
                                    >
                                        {r.url}
                                    </a>
                                    <p>{truncate(r.description)}</p>
                                </div>
                            ))}
                        </div>
                    ))}
                </div>
            </div>
        </>
    );
}