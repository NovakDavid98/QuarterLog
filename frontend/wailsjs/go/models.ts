export namespace config {
	
	export class Config {
	    miniMaxApiKey: string;
	    miniMaxBaseUrl: string;
	    miniMaxModel: string;
	    filePath: string;
	    categories: string;
	    types: string;
	    intervalMinutes: number;
	    monitor: number;
	    popupPosition: string;
	    language: string;
	    prompt: string;
	    paused: boolean;
	    autostart: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.miniMaxApiKey = source["miniMaxApiKey"];
	        this.miniMaxBaseUrl = source["miniMaxBaseUrl"];
	        this.miniMaxModel = source["miniMaxModel"];
	        this.filePath = source["filePath"];
	        this.categories = source["categories"];
	        this.types = source["types"];
	        this.intervalMinutes = source["intervalMinutes"];
	        this.monitor = source["monitor"];
	        this.popupPosition = source["popupPosition"];
	        this.language = source["language"];
	        this.prompt = source["prompt"];
	        this.paused = source["paused"];
	        this.autostart = source["autostart"];
	    }
	}

}

export namespace main {
	
	export class PendingView {
	    id: string;
	    date: string;
	    from: string;
	    to: string;
	    hours: number;
	    status: string;
	    thumb: string;
	    locked: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PendingView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.date = source["date"];
	        this.from = source["from"];
	        this.to = source["to"];
	        this.hours = source["hours"];
	        this.status = source["status"];
	        this.thumb = source["thumb"];
	        this.locked = source["locked"];
	    }
	}

}

export namespace minimax {
	
	export class Suggestion {
	    description: string;
	    type: string;
	
	    static createFrom(source: any = {}) {
	        return new Suggestion(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.description = source["description"];
	        this.type = source["type"];
	    }
	}

}

