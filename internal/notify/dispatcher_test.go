package notify

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Fake Sender ---

type fakeSender struct {
	channel Channel
	calls   []Message
	err     error
}

func (s *fakeSender) Send(_ context.Context, msg Message) (*DeliveryResult, error) {
	s.calls = append(s.calls, msg)
	if s.err != nil {
		return nil, s.err
	}
	return &DeliveryResult{
		MessageID:  "msg-123",
		ChannelRef: "ref-456",
		Channel:    s.channel,
		Success:    true,
		SentAt:     time.Now(),
	}, nil
}

func (s *fakeSender) Name() Channel {
	return s.channel
}

// --- Tests ---

func TestDispatcher_RegisterAndDispatch(t *testing.T) {
	d := NewDispatcher()
	sender := &fakeSender{channel: ChannelSlack}
	d.Register(sender)

	msg := Message{
		AlertID: "alert-1",
		UserID:  "user-1",
		Channel: ChannelSlack,
		Address: "U12345",
		Title:   "CPU high",
		Body:    "CPU > 90%",
	}

	result, err := d.Dispatch(context.Background(), msg)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Success)
	assert.Equal(t, ChannelSlack, result.Channel)
	assert.Equal(t, "msg-123", result.MessageID)
	assert.Equal(t, "ref-456", result.ChannelRef)

	require.Len(t, sender.calls, 1)
	assert.Equal(t, "alert-1", sender.calls[0].AlertID)
	assert.Equal(t, "U12345", sender.calls[0].Address)
}

func TestDispatcher_UnknownChannel(t *testing.T) {
	d := NewDispatcher()

	msg := Message{
		AlertID: "alert-1",
		Channel: ChannelSMS,
	}

	result, err := d.Dispatch(context.Background(), msg)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "no sender registered")
	assert.Contains(t, err.Error(), "sms")
}

func TestDispatcher_AvailableChannels(t *testing.T) {
	d := NewDispatcher()
	d.Register(&fakeSender{channel: ChannelSlack})
	d.Register(&fakeSender{channel: ChannelEmail})
	d.Register(&fakeSender{channel: ChannelSMS})

	channels := d.AvailableChannels()
	sort.Slice(channels, func(i, j int) bool { return channels[i] < channels[j] })

	require.Len(t, channels, 3)
	assert.Equal(t, ChannelEmail, channels[0])
	assert.Equal(t, ChannelSlack, channels[1])
	assert.Equal(t, ChannelSMS, channels[2])
}

func TestDispatcher_DispatchFailure(t *testing.T) {
	d := NewDispatcher()
	senderErr := errors.New("connection refused")
	sender := &fakeSender{channel: ChannelEmail, err: senderErr}
	d.Register(sender)

	msg := Message{
		AlertID: "alert-1",
		Channel: ChannelEmail,
		Address: "alice@example.com",
	}

	result, err := d.Dispatch(context.Background(), msg)

	require.Error(t, err)
	assert.Equal(t, senderErr, err)
	require.NotNil(t, result)
	assert.False(t, result.Success)
	assert.Equal(t, "connection refused", result.Error)
	assert.Equal(t, ChannelEmail, result.Channel)
}

func TestDispatcher_DispatchWithRetry_UnknownChannel(t *testing.T) {
	d := NewDispatcher()

	msg := Message{
		AlertID: "alert-1",
		Channel: ChannelVoice,
	}

	result, err := d.DispatchWithRetry(context.Background(), msg, "", nil)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "no sender registered")
}

func TestDispatcher_DispatchWithRetry_NonTransientFailsImmediately(t *testing.T) {
	d := NewDispatcher()
	sender := &fakeSender{
		channel: ChannelSlack,
		err:     errors.New("invalid token"),
	}
	d.Register(sender)

	msg := Message{
		AlertID: "alert-1",
		Channel: ChannelSlack,
	}

	result, err := d.DispatchWithRetry(context.Background(), msg, "", nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid token")
	require.NotNil(t, result)
	assert.False(t, result.Success)
	assert.Equal(t, 0, result.RetryCount, "non-transient error should not retry")
	require.Len(t, sender.calls, 1)
}

func TestDispatcher_RegisterOverwrites(t *testing.T) {
	d := NewDispatcher()

	sender1 := &fakeSender{channel: ChannelSlack, err: errors.New("old sender")}
	sender2 := &fakeSender{channel: ChannelSlack}
	d.Register(sender1)
	d.Register(sender2)

	msg := Message{
		AlertID: "alert-1",
		Channel: ChannelSlack,
	}

	result, err := d.Dispatch(context.Background(), msg)

	require.NoError(t, err)
	assert.True(t, result.Success)
	// sender2 should have been called, not sender1.
	assert.Len(t, sender1.calls, 0)
	assert.Len(t, sender2.calls, 1)
}

func TestDispatcher_EmptyChannels(t *testing.T) {
	d := NewDispatcher()
	channels := d.AvailableChannels()
	assert.Empty(t, channels)
}
