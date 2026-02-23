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
 * @typedef {Object} DeliveredPacket
 * @property {string} delivered_at
 * @property {string} target_server
 * @property {string} payload
 * @property {number} payload_size
 */

/**
 * Composable for real-time GPS packet streams via Socket.IO (received + delivered).
 * @returns {{ packets: import('vue').Ref<GpsPacket[]>, deliveredPackets: import('vue').Ref<DeliveredPacket[]>, connected: import('vue').Ref<boolean>, error: import('vue').Ref<string|null>, socketId: import('vue').Ref<string|null>, clearPackets: () => void, clearDeliveredPackets: () => void }}
 */
export function useGpsPackets() {
  const packets = ref(/** @type {GpsPacket[]} */ ([]))
  const deliveredPackets = ref(/** @type {DeliveredPacket[]} */ ([]))

  function clearPackets() {
    packets.value = []
  }
  function clearDeliveredPackets() {
    deliveredPackets.value = []
  }
  const connected = ref(false)
  const error = ref(/** @type {string|null} */ (null))
  const socketId = ref(/** @type {string|null} */ (null))

  /** @type {ReturnType<typeof io> | null} */
  let socket = null

  onMounted(() => {
    if (DEBUG) {
      console.log('[Socket.IO] Connecting to path: /socket.io/')
    }
    // Go server (ismhdez/socket.io-golang) supports WebSocket only, not polling
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
        packets.value = [
          {
            message_id: data.message_id ?? '',
            received_at: data.received_at ?? new Date().toISOString(),
            payload: data.payload ?? '',
            payload_size: typeof data.payload_size === 'number' ? data.payload_size : 0,
          },
          ...packets.value,
        ].slice(0, MAX_PACKETS)
      }
    })

    socket.on('gps-delivered', (data) => {
      if (DEBUG) {
        console.log('[Socket.IO] gps-delivered received', data)
      }
      if (data && typeof data === 'object') {
        deliveredPackets.value = [
          {
            delivered_at: data.delivered_at ?? new Date().toISOString(),
            target_server: data.target_server ?? '',
            payload: data.payload ?? '',
            payload_size: typeof data.payload_size === 'number' ? data.payload_size : 0,
          },
          ...deliveredPackets.value,
        ].slice(0, MAX_PACKETS)
      }
    })
  })

  onUnmounted(() => {
    if (socket) {
      socket.disconnect()
      socket = null
    }
  })

  return { packets, deliveredPackets, connected, error, socketId, clearPackets, clearDeliveredPackets }
}
