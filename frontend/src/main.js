import './style.css';
import { GetConnectionStatus } from '../wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/runtime/runtime';

// Initial HTML structure
document.querySelector('#app').innerHTML = `
    <div class="overlay-box" id="overlay-box">
        <div class="header drag-region">
            <h1>GhostDraft</h1>
        </div>

        <div class="status-card" id="status-card">
            <div class="status-indicator">
                <div class="status-dot waiting" id="status-dot"></div>
                <span class="status-message" id="status-message">Initializing...</span>
            </div>
        </div>

        <div class="build-card hidden" id="build-card">
            <div class="build-matchup">
                <span class="winrate-label" id="winrate-label">Win Rate</span>
                <span class="build-winrate" id="build-winrate"></span>
            </div>
        </div>
    </div>
`;

const statusDot = document.getElementById('status-dot');
const statusMessage = document.getElementById('status-message');
const statusCard = document.getElementById('status-card');
const buildCard = document.getElementById('build-card');
const buildWinrate = document.getElementById('build-winrate');
const winrateLabel = document.getElementById('winrate-label');

// Update UI based on connection status
function updateStatus(status) {
    statusMessage.textContent = status.message;

    if (status.connected) {
        statusDot.className = 'status-dot connected';
    } else {
        statusDot.className = 'status-dot waiting';
    }
}

// Update UI based on champ select status
function updateChampSelect(data) {
    if (!data.inChampSelect) {
        buildCard.classList.add('hidden');
        statusCard.classList.remove('hidden');
        return;
    }
    // Keep status visible, build card will show when matchup is ready
}

// Update UI based on build data
function updateBuild(data) {
    if (!data.hasBuild) {
        buildCard.classList.add('hidden');
        return;
    }

    buildCard.classList.remove('hidden');
    statusCard.classList.add('hidden');

    winrateLabel.textContent = data.winRateLabel || 'Win Rate';
    buildWinrate.textContent = data.winRate;

    // Update matchup status styling
    buildWinrate.classList.remove('winning', 'losing', 'even');
    if (data.matchupStatus) {
        buildWinrate.classList.add(data.matchupStatus);
    }
}

// Listen for status updates from backend
EventsOn('lcu:status', (status) => {
    updateStatus(status);
});

// Listen for champ select updates
EventsOn('champselect:update', (data) => {
    updateChampSelect(data);
});

// Listen for build updates
EventsOn('build:update', (data) => {
    updateBuild(data);
});

// Get initial status
GetConnectionStatus()
    .then(updateStatus)
    .catch(err => {
        console.error('Failed to get connection status:', err);
        updateStatus({ connected: false, message: 'Waiting for League...' });
    });
