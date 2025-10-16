"use client";

import * as React from "react";
import { GitHubLogoIcon } from "@radix-ui/react-icons";

import { Button } from "@/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
  TooltipProvider
} from "@/components/ui/tooltip";

export function GithubLink() {
  return (
    <TooltipProvider disableHoverableContent>
      <Tooltip delayDuration={100}>
        <TooltipTrigger asChild>
          <Button
            className="relative flex h-9 w-9 items-center justify-center rounded-full border border-input bg-background shadow-sm transition hover:bg-accent hover:text-accent-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
            variant="outline"
            size="icon"
            asChild
          >
            <a
              href="https://github.com/ChouHX/WireguardManager"
              target="_blank"
              rel="noopener noreferrer"
            >
              <GitHubLogoIcon className="w-[1.2rem] h-[1.2rem]" />
              <span className="sr-only">GitHub Repository</span>
            </a>
          </Button>
        </TooltipTrigger>
        <TooltipContent side="bottom">GitHub Repository</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
