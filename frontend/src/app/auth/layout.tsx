import { Metadata } from "next";
import Image from "next/image";
import { LanguageToggle } from "@/components/language-toggle";
import { ModeToggle } from "@/components/mode-toggle";
import { APP_CONFIG } from "@/config/app";

export const metadata: Metadata = {
  title: APP_CONFIG.title,
  description: APP_CONFIG.description,
};

export default function AuthLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <div className="min-h-screen flex items-center justify-center bg-background">
      {/* Language and Theme toggles */}
      <div className="absolute top-4 right-4 flex items-center space-x-2">
        <LanguageToggle />
        <ModeToggle />
      </div>
      
      {/* Center content with logo and form */}
      <div className="w-full max-w-md px-4 py-12">
        <div className="flex flex-col items-center space-y-8">
          {/* Logo */}
          <div className="flex flex-col items-center space-y-4">
            <Image 
              src="/logo.png" 
              alt="WireGuard Manager" 
              width={80} 
              height={80}
              className="rounded-lg"
            />
            <h1 className="text-2xl font-bold text-center">{APP_CONFIG.title}</h1>
          </div>
          
          {/* Form content */}
          <div className="w-full">
            {children}
          </div>
        </div>
      </div>
    </div>
  );
}

