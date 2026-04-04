export class Ticker {
    constructor(elementId) {
        this.container = document.getElementById(elementId);
        this.textElement = this.container ? this.container.querySelector('p') : null;
        this.messages = [];
        this.cannedMessages = [
            "Scanning horizons...",
            "Checking weather charts...",
            "Contacting local marinas...",
            "Analyzing tide patterns...",
            "Reviewing pilot charts...",
            "Hailing the harbor master...",
            "Plotting courses...",
            "Checking depth soundings...",
            "Consulting local guides..."
        ];
        this.interval = null;
        this.currentIndex = 0;
        this.isRunning = false;
    }

    start() {
        if (!this.container || !this.textElement) return;
        this.container.classList.remove('hidden');
        this.isRunning = true;
        this.cycle();
        if (this.interval) clearInterval(this.interval);
        this.interval = setInterval(() => this.cycle(), 3000);
    }

    stop() {
        if (!this.container) return;
        if (this.interval) clearInterval(this.interval);
        this.container.classList.add('hidden');
        this.isRunning = false;
        // Reset color
        if (this.textElement) this.textElement.style.color = '';
    }

    error(msg) {
        if (!this.container || !this.textElement) return;
        this.container.classList.remove('hidden');
        this.isRunning = true;
        if (this.interval) clearInterval(this.interval);
        
        this.textElement.style.color = 'var(--brand-red, #d9534f)';
        this.show(msg);
        
        // Stop after 5 seconds
        setTimeout(() => this.stop(), 5000);
    }

    push(msg) {
        if (!this.isRunning) return;
        
        // Show immediate update
        this.show(msg);
        
        // Reset timer to give it time to be read
        if (this.interval) clearInterval(this.interval);
        this.interval = setInterval(() => this.cycle(), 4000); // Give it a bit longer
    }

    cycle() {
        const msg = this.cannedMessages[this.currentIndex];
        this.currentIndex = (this.currentIndex + 1) % this.cannedMessages.length;
        this.show(msg);
    }

    show(text) {
        if (!this.textElement) return;
        
        // Simple fade out/in effect
        this.textElement.style.opacity = 0;
        setTimeout(() => {
            this.textElement.textContent = text;
            this.textElement.style.opacity = 1;
        }, 300);
    }
}
