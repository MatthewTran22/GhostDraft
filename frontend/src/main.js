import './style.css';
import { GetConnectionStatus } from '../wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/runtime/runtime';

// Initial HTML structure
document.querySelector('#app').innerHTML = `
    <div class="header">
        <h1>GhostDraft</h1>
        <div class="subtitle">League of Legends Draft Assistant</div>
    </div>

    <div class="status-card">
        <div class="status-indicator">
            <div class="status-dot waiting" id="status-dot"></div>
            <span class="status-message" id="status-message">Initializing...</span>
        </div>
        <div class="status-port" id="status-port"></div>
    </div>

    <div class="champ-select-card hidden" id="champ-select-card">
        <div class="champ-select-header">
            <span class="champ-select-badge">IN CHAMPION SELECT</span>
            <span class="champ-select-timer" id="champ-select-timer"></span>
        </div>
        <div class="champ-select-content">
            <div class="champ-info">
                <div class="champ-label">Phase</div>
                <div class="champ-value" id="champ-phase">-</div>
            </div>
            <div class="champ-info">
                <div class="champ-label">Position</div>
                <div class="champ-value" id="champ-position">-</div>
            </div>
            <div class="champ-info">
                <div class="champ-label">Champion</div>
                <div class="champ-value" id="champ-name">-</div>
            </div>
            <div class="champ-info">
                <div class="champ-label">Action</div>
                <div class="champ-value" id="champ-action">-</div>
            </div>
        </div>
    </div>

    <div class="build-card hidden" id="build-card">
        <div class="build-header">
            <span class="build-title" id="build-title">Build</span>
        </div>
        <div class="build-matchup">
            <span class="winrate-label" id="winrate-label">Win Rate</span>
            <span class="build-winrate" id="build-winrate"></span>
        </div>
        <div class="build-footer" id="build-footer"></div>
    </div>

    <div class="content" id="content-placeholder">
        <div class="placeholder">
            Hover a champion to see recommended builds
        </div>
    </div>
`;

const statusDot = document.getElementById('status-dot');
const statusMessage = document.getElementById('status-message');
const statusPort = document.getElementById('status-port');
const champSelectCard = document.getElementById('champ-select-card');
const buildCard = document.getElementById('build-card');
const contentPlaceholder = document.getElementById('content-placeholder');
const champPhase = document.getElementById('champ-phase');
const champPosition = document.getElementById('champ-position');
const champName = document.getElementById('champ-name');
const champAction = document.getElementById('champ-action');
const champTimer = document.getElementById('champ-select-timer');

// Build elements
const buildTitle = document.getElementById('build-title');
const buildWinrate = document.getElementById('build-winrate');
const winrateLabel = document.getElementById('winrate-label');
const buildFooter = document.getElementById('build-footer');

// Update UI based on connection status
function updateStatus(status) {
    statusMessage.textContent = status.message;

    if (status.connected) {
        statusDot.className = 'status-dot connected';
        statusPort.textContent = `Port: ${status.port}`;
    } else {
        statusDot.className = 'status-dot waiting';
        statusPort.textContent = '';
    }
}

// Update UI based on champ select status
function updateChampSelect(data) {
    if (!data.inChampSelect) {
        champSelectCard.classList.add('hidden');
        buildCard.classList.add('hidden');
        contentPlaceholder.classList.remove('hidden');
        return;
    }

    champSelectCard.classList.remove('hidden');
    contentPlaceholder.classList.add('hidden');

    // Update phase
    champPhase.textContent = data.phase || '-';

    // Update position
    const positionMap = {
        'top': 'Top',
        'jungle': 'Jungle',
        'middle': 'Mid',
        'bottom': 'Bot',
        'utility': 'Support',
        '': '-'
    };
    champPosition.textContent = positionMap[data.localPosition] || data.localPosition || '-';

    // Update champion name
    if (data.championName) {
        if (data.isLocked) {
            champName.textContent = `${data.championName} (Locked)`;
        } else {
            champName.textContent = data.championName;
        }
    } else {
        champName.textContent = 'None';
    }

    // Update action type
    const actionMap = {
        'pick': 'Picking',
        'ban': 'Banning',
        '': '-'
    };
    champAction.textContent = actionMap[data.actionType] || data.actionType || '-';

    // Update timer
    if (data.timeLeft > 0) {
        champTimer.textContent = `${data.timeLeft}s`;
    } else {
        champTimer.textContent = '';
    }
}

// Update UI based on build data
function updateBuild(data) {
    if (!data.hasBuild) {
        buildCard.classList.add('hidden');
        if (data.error) {
            contentPlaceholder.classList.remove('hidden');
            contentPlaceholder.querySelector('.placeholder').textContent = `Stats unavailable: ${data.error}`;
        }
        return;
    }

    buildCard.classList.remove('hidden');
    contentPlaceholder.classList.add('hidden');

    // Update header
    buildTitle.textContent = `${data.championName}`;
    winrateLabel.textContent = data.winRateLabel || 'Win Rate';
    buildWinrate.textContent = data.winRate;

    // Footer
    buildFooter.textContent = `Data from U.GG • Patch ${data.patch}`;
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
