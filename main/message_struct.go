package main // Use the main package so this file compiles with the app entrypoint.

type Message struct { // Define one message record as stored in PostgreSQL.
	ID        int    // Store auto-increment database primary key.
	Sender    string // Store sender phone number only (without @s.whatsapp.net suffix).
	Receiver  string // Store receiver phone number only (without @s.whatsapp.net suffix).
	Content   string // Store human-readable message text/caption.
	Timestamp string // Store insert timestamp (filled by database default now()).
	Direction string // Store message direction: inbound or outbound.
}
