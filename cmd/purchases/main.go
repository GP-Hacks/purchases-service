package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/GP-Hacks/kdt2024-commons/prettylogger"
	"github.com/GP-Hacks/kdt2024-purchases/config"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/streadway/amqp"
	"log/slog"
	"time"
)

type PurchaseMessage struct {
	UserToken    string    `json:"user_token"`
	PlaceID      int       `json:"place_id"`
	EventTime    time.Time `json:"event_time"`
	PurchaseTime time.Time `json:"purchase_time"`
	Cost         int       `json:"cost"`
}

type DonationMessage struct {
	UserToken    string    `json:"user_token"`
	CollectionID int       `json:"collection_id"`
	DonationTime time.Time `json:"donation_time"`
	Amount       int       `json:"amount"`
}

func main() {
	cfg := config.MustLoad()
	log := prettylogger.SetupLogger(cfg.Env)
	log.Info("Configuration loaded")
	log.Info("Logger initialized")

	dbpool, err := pgxpool.New(context.Background(), cfg.PostgresAddress+"?sslmode=disable")
	if err != nil {
		log.Error("Postgres connection error", slog.String("error", err.Error()))
		return
	}
	log.Info("Postgres connected")

	if err := createTables(context.Background(), dbpool, log); err != nil {
		log.Error("Failed to create necessary tables", slog.String("error", err.Error()))
		return
	}

	conn, err := amqp.Dial(cfg.RabbitMQAddress)
	if err != nil {
		log.Error("RabbitMQ connection error", slog.String("error", err.Error()))
		return
	}
	defer func() {
		if err := conn.Close(); err != nil {
			log.Error("Error closing RabbitMQ connection", slog.String("error", err.Error()))
		} else {
			log.Info("RabbitMQ connection closed")
		}
	}()

	ch, err := conn.Channel()
	if err != nil {
		log.Error("Failed to open RabbitMQ channel", slog.String("error", err.Error()))
		return
	}
	defer func() {
		if err := ch.Close(); err != nil {
			log.Error("Error closing RabbitMQ channel", slog.String("error", err.Error()))
		} else {
			log.Info("RabbitMQ channel closed")
		}
	}()

	msgs, err := ch.Consume(
		cfg.QueueName,
		"",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Error("Failed to register RabbitMQ consumer", slog.String("error", err.Error()))
		return
	}

	log.Info("RabbitMQ connected and consuming messages")

	for msg := range msgs {
		if err := processMessage(context.Background(), msg, dbpool, log); err != nil {
			log.Error("Message processing error", slog.String("error", err.Error()))
		}
	}
}

func createTables(ctx context.Context, dbpool *pgxpool.Pool, log *slog.Logger) error {
	const createTablesQuery = `
		CREATE TABLE IF NOT EXISTS ticket_purchases (
			user_token TEXT,
			place_id INT,
			event_time TIMESTAMP,
			purchase_time TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			cost INT
		);
		CREATE TABLE IF NOT EXISTS donations (
			user_token TEXT,
			collection_id INT,
			donation_time TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			amount INT
		);
	`
	_, err := dbpool.Exec(ctx, createTablesQuery)
	if err != nil {
		return fmt.Errorf("failed to create tables: %w", err)
	}
	log.Info("Tables created or already exist")
	return nil
}

func processMessage(ctx context.Context, msg amqp.Delivery, dbpool *pgxpool.Pool, log *slog.Logger) error {
	var messageType map[string]interface{}
	if err := json.Unmarshal(msg.Body, &messageType); err != nil {
		return fmt.Errorf("failed to unmarshal message body: %w", err)
	}

	if _, ok := messageType["place_id"]; ok {
		return processPurchaseMessage(ctx, msg.Body, dbpool, log)
	} else if _, ok := messageType["collection_id"]; ok {
		return processDonationMessage(ctx, msg.Body, dbpool, log)
	} else {
		log.Warn("Received message with unknown type", slog.String("body", string(msg.Body)))
		return nil
	}
}

func processPurchaseMessage(ctx context.Context, body []byte, dbpool *pgxpool.Pool, log *slog.Logger) error {
	var dbmsg PurchaseMessage
	if err := json.Unmarshal(body, &dbmsg); err != nil {
		return fmt.Errorf("failed to unmarshal purchase message: %w", err)
	}

	if dbmsg.UserToken == "" || dbmsg.PlaceID == 0 || dbmsg.EventTime.IsZero() || dbmsg.Cost == 0 {
		log.Warn("Received invalid purchase message", slog.Any("message", dbmsg))
		return nil
	}

	_, err := dbpool.Exec(ctx, `INSERT INTO ticket_purchases(user_token, place_id, event_time, purchase_time, cost) VALUES ($1, $2, $3, $4, $5)`,
		dbmsg.UserToken, dbmsg.PlaceID, dbmsg.EventTime, dbmsg.PurchaseTime, dbmsg.Cost)
	if err != nil {
		return fmt.Errorf("failed to insert purchase message into Postgres: %w", err)
	}
	log.Info("Saved ticket purchase", slog.Any("purchase_message", dbmsg))
	return nil
}

func processDonationMessage(ctx context.Context, body []byte, dbpool *pgxpool.Pool, log *slog.Logger) error {
	var dbmsg DonationMessage
	if err := json.Unmarshal(body, &dbmsg); err != nil {
		return fmt.Errorf("failed to unmarshal donation message: %w", err)
	}

	if dbmsg.UserToken == "" || dbmsg.CollectionID == 0 || dbmsg.Amount == 0 {
		log.Warn("Received invalid donation message", slog.Any("message", dbmsg))
		return nil
	}

	_, err := dbpool.Exec(ctx, `INSERT INTO donations(user_token, collection_id, donation_time, amount) VALUES ($1, $2, $3, $4)`,
		dbmsg.UserToken, dbmsg.CollectionID, dbmsg.DonationTime, dbmsg.Amount)
	if err != nil {
		return fmt.Errorf("failed to insert donation message into Postgres: %w", err)
	}
	log.Info("Saved donation", slog.Any("donation_message", dbmsg))
	return nil
}
