import { ref, onMounted, onUnmounted } from 'vue'
import { io } from 'socket.io-client'

const MAX_PACKETS = 15
const DEBUG = import.meta.env.DEV

/**
 * @typedef {Object} GpsPacket
 * @property {string} message_id
 * @property {string} received_at
 * @property {string} payload
 * @property {number} payload_size
 */

/**
 * @typedef {'sending' | 'delivered' | 'failed'} DeliveryStatus
 */

/**
 * @typedef {Object} DeliveryBatch
 * @property {string} delivery_id
 * @property {DeliveryStatus} status
 * @property {string} target_server
 * @property {string} payload
 * @property {number} payload_size
 * @property {number} record_count
 * @property {string} updated_at
 */

/**
 * Composable for real-time GPS packet streams via Socket.IO (received + delivery status).
 * @returns {{ packets: import('vue').Ref<GpsPacket[]>, deliveryBatches: import('vue').Ref<DeliveryBatch[]>, connected: import('vue').Ref<boolean>, error: import('vue').Ref<string|null>, socketId: import('vue').Ref<string|null>, clearPackets: () => void, clearDeliveryBatches: () => void }}
 */
export function useGpsPackets() {
  const packets = ref(/** @type {GpsPacket[]} */ ([]))
  const deliveryBatches = ref(/** @type {DeliveryBatch[]} */ ([]))

  function clearPackets() {
    packets.value = []
  }
  function clearDeliveryBatches() {
    deliveryBatches.value = []
  }
  const connected = ref(false)
  const error = ref(/** @type {string|null} */ (null))
  const socketId = ref(/** @type {string|null} */ (null))

  /** @type {ReturnType<typeof io> | null} */
  let socket = null

  function upsertDeliveryBatch(data) {
    const batch = {
      delivery_id: data.delivery_id ?? '',
      status: data.status ?? 'sending',
      target_server: data.target_server ?? '',
      payload: data.payload ?? '',
      payload_size: typeof data.payload_size === 'number' ? data.payload_size : 0,
      record_count: typeof data.record_count === 'number' ? data.record_count : 0,
      updated_at: data.updated_at ?? new Date().toISOString(),
    }

    const index = deliveryBatches.value.findIndex((b) => b.delivery_id === batch.delivery_id)
    if (index >= 0) {
      const next = [...deliveryBatches.value]
      next[index] = { ...next[index], ...batch }
      deliveryBatches.value = next
    } else {
      deliveryBatches.value = [batch, ...deliveryBatches.value].slice(0, MAX_PACKETS)
    }
  }

  onMounted(() => {
    if (DEBUG) {
      console.log('[Socket.IO] Connecting to path: /socket.io/')
    }
    socket = io({
      path: '/socket.io/',
      transports: ['websocket'],
    })

    socket.on('connect', () => {
      connected.value = true
      error.value = null
      socketId.value = socket?.id ?? null
      if (DEBUG) {
        console.log('[Socket.IO] Connected', { id: socket?.id, transport: socket?.io?.engine?.transport?.name })
      }
    })

    socket.on('disconnect', (reason) => {
      connected.value = false
      socketId.value = null
      if (reason === 'io server disconnect') {
        error.value = 'Server disconnected'
      } else if (reason === 'io client disconnect') {
        error.value = null
      } else {
        error.value = 'Connection lost'
      }
      if (DEBUG) {
        console.log('[Socket.IO] Disconnected', { reason })
      }
    })

    socket.on('connect_error', (err) => {
      connected.value = false
      error.value = err.message || 'Connection failed'
      if (DEBUG) {
        console.error('[Socket.IO] Connect error', err)
      }
    })

    socket.on('gps-packet', (data) => {
      if (DEBUG) {
        console.log('[Socket.IO] gps-packet received', data)
      }
      if (data && typeof data === 'object') {
        const rawPayload = data.payload
        packets.value = [
          {
            message_id: data.message_id ?? '',
            received_at: data.received_at ?? new Date().toISOString(),
            payload:
              typeof rawPayload === 'string'
                ? rawPayload
                : rawPayload != null
                  ? JSON.stringify(rawPayload)
                  : '',
            payload_size: typeof data.payload_size === 'number' ? data.payload_size : 0,
          },
          ...packets.value,
        ].slice(0, MAX_PACKETS)
      }
    })

    socket.on('gps-delivery', (data) => {
      if (DEBUG) {
        console.log('[Socket.IO] gps-delivery received', data)
      }
      if (data && typeof data === 'object') {
        upsertDeliveryBatch(data)
      }
    })

    // Backward compatibility with older server events
    socket.on('gps-delivered', (data) => {
      if (DEBUG) {
        console.log('[Socket.IO] gps-delivered received', data)
      }
      if (data && typeof data === 'object') {
        upsertDeliveryBatch({
          delivery_id: `${data.delivered_at ?? Date.now()}-${data.target_server ?? ''}`,
          status: 'delivered',
          target_server: data.target_server ?? '',
          payload: data.payload ?? '',
          payload_size: data.payload_size,
          record_count: data.record_count,
          updated_at: data.delivered_at ?? new Date().toISOString(),
        })
      }
    })
  })

  onUnmounted(() => {
    if (socket) {
      socket.disconnect()
      socket = null
    }
  })

  return {
    packets,
    deliveryBatches,
    connected,
    error,
    socketId,
    clearPackets,
    clearDeliveryBatches,
  }
}
